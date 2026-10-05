package prioritybox

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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongiddd"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch precedes any state read:
	// a valid enqueue followed by an unknown kind must not commit anything.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(9), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) || len(q.Snapshot().Items) != 0 {
		t.Fatal(e, q.Snapshot())
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
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestExistsAndRevisionRollback(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 || len(s.Items) != 1 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != 0 || got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Items) != 1 {
		t.Fatal(got)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Now != 2 {
		t.Fatal(q.Snapshot())
	}
	r, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0}, {Enqueue, "d", 5, 0},
	}})
	x, e := q.Pop(0, 10)
	if e != nil || len(x) != 3 || x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(1, 1)
	if len(x) != 1 || x[0].ID != "b" {
		t.Fatal(x)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
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
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
