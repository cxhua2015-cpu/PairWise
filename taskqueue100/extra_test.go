package taskqueue100

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "01234567", "z9-_"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before state reads:
	// unknown kind later in the batch must win over ErrExists earlier.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(9), "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 0 || q.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Now: 6, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 1 || q.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestRevisionRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Failing batch must not consume revisions.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().NextRevision != 2 {
		t.Fatal(q.Snapshot().NextRevision)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	// Cancel-then-enqueue within one batch: capacity checked only at the end.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestDuplicateAndCancel(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 3, 0}}}); e != nil {
		t.Fatal(e)
	}
	got := q.Snapshot().Items
	if len(got) != 1 || got[0].Priority != 3 || got[0].Revision != 2 {
		t.Fatal(got)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
		{Enqueue, "e", 9, 3},
	}})
	got, e := q.Pop(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b"}) {
		t.Fatal(ids)
	}
	// "e" is not ready yet; "a" remains.
	got, e = q.Pop(1, 3)
	if e != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(3, 1)
	if e != nil || len(got) != 1 || got[0].ID != "e" {
		t.Fatal(got, e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "x"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-%04d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i % 3)}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	var prev *Item
	for i := range s.Items {
		it := s.Items[i]
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
		if prev != nil && lessCanonical(it, *prev) {
			t.Fatal("snapshot not in canonical order")
		}
		prev = &s.Items[i]
	}
	if int(s.NextRevision) < 1 {
		t.Fatal(s.NextRevision)
	}
}
