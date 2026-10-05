package dispatchbox

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongiddd"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestBatchValidatedBeforeState(t *testing.T) {
	q := queue(t)
	// Unknown kind anywhere in the batch fails before any state read,
	// even if an earlier op would hit ErrExists.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind(99), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
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
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != r1.Revision+1 || len(s.Items) != 1 || s.Generation != 1 {
		t.Fatal(s)
	}
	// Failed enqueue must not consume a revision.
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 0, 0}}})
	if r2.Revision != r1.Revision+1 {
		t.Fatal(r1, r2)
	}
}

func TestExistsAndGeneration(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g := q.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
	// Empty batch succeeds without bumping generation.
	if _, e := q.Apply(Batch{Now: 1}); e != nil {
		t.Fatal(e)
	}
	if g := q.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestPopReadinessAndOrder(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "future", 9, 10},
		{Enqueue, "lo", 1, 0},
		{Enqueue, "hi2", 5, 1},
		{Enqueue, "hi1", 5, 0},
	}})
	x, e := q.Pop(5, 10)
	if e != nil || len(x) != 3 || x[0].ID != "hi1" || x[1].ID != "hi2" || x[2].ID != "lo" {
		t.Fatal(e, x)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "x"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 1000, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(n, 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
	}
	if int(s.NextRevision) < 1 {
		t.Fatal(s.NextRevision)
	}
}
