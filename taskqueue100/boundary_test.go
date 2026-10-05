package taskqueue100

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "é"}
	for _, id := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegativeReadyAt(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestFailedBatchDoesNotAdvanceTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestDuplicateAndExists(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestRevisionAssignmentAndRollback(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 {
		t.Fatal(s.NextRevision)
	}
	revs := map[string]uint64{}
	for _, it := range s.Items {
		revs[it.ID] = it.Revision
	}
	if revs["a"] != 1 || revs["b"] != 2 {
		t.Fatal(revs)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().NextRevision != 3 {
		t.Fatal(q.Snapshot().NextRevision)
	}
}

func TestGenerationSemantics(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	if _, e = q.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "nope", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().Generation != 1 {
		t.Fatal(q.Snapshot().Generation)
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, e := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	// Temporarily exceed capacity mid-batch, end within it: must succeed.
	if _, e = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0},
	}}); e != nil {
		t.Fatal(e)
	}
	// End above capacity: whole batch rolls back.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := len(q.Snapshot().Items); got != 2 {
		t.Fatal(got)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "p1a", 1, 5},
		{Enqueue, "p2b", 2, 9},
		{Enqueue, "p2a", 2, 3},
		{Enqueue, "p1b", 1, 5},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(9, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"p2a", "p2b", "p1a", "p1b"}
	ids := []string{}
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatal(ids)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestPopLimitAndInvalid(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, -3); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	got, e := q.Pop(0, 1)
	if e != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(0, 5)
	if e != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(0, 5)
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "fake"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestCancelThenReenqueue(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 5, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	got, _ := q.Pop(0, 1)
	if len(got) != 1 || got[0].Priority != 5 || got[0].Revision != r.Revision {
		t.Fatal(got, r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 4096, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("g%d-i%03d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 4096 {
		t.Fatal(len(s.Items))
	}
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
	}
}
