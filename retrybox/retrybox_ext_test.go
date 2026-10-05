package retrybox

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid9"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndBatchAtomicity(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation happens before state reads: an unknown kind
	// must fail even if an earlier op would hit a state error.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind(9), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
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
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestFailedBatchRollsBackRevisionAndTime(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 0, 0}}})
	// Fails at final capacity check after consuming revisions.
	_, e := q.Apply(Batch{Now: 7, Ops: []Op{{Enqueue, "b", 0, 0}, {Enqueue, "c", 0, 0}, {Enqueue, "d", 0, 0}, {Enqueue, "e", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.Now != 3 || s.NextRevision != 2 || s.Generation != r1.Generation || len(s.Items) != 1 {
		t.Fatalf("%+v", s)
	}
	// Next successful enqueue reuses the rolled-back revision.
	r2, _ := q.Apply(Batch{Now: 8, Ops: []Op{{Enqueue, "b", 0, 0}}})
	if r2.Revision != 2 || r2.Generation != r1.Generation+1 {
		t.Fatalf("%+v", r2)
	}
}

func TestDuplicateAndCancelSemantics(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Enqueue then cancel the same ID within one batch succeeds.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 0, 0}, {Cancel, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(len(q.Snapshot().Items))
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 9, 9},
		{Enqueue, "z", 9, 2},
		{Enqueue, "w", 9, 2},
	}})
	got, e := q.Pop(5, 10)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	if got[0].ID != "w" || got[1].ID != "z" || got[2].ID != "x" {
		t.Fatal(got)
	}
	// "y" was not ready at now=5 and must remain.
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "y" {
		t.Fatalf("%+v", s)
	}
	if got, _ = q.Pop(9, 1); len(got) != 1 || got[0].ID != "y" {
		t.Fatal(got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 || q.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	if q.Snapshot().Now != 2 {
		t.Fatal("time should advance on empty batch")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "hack"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatalf("%+v", got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 512, MaxIDBytes: 8})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%dn%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 512 {
		t.Fatal(len(s.Items))
	}
	seen := map[uint64]bool{}
	for _, it := range s.Items {
		if seen[it.Revision] {
			t.Fatal("duplicate revision", it.Revision)
		}
		seen[it.Revision] = true
	}
}
