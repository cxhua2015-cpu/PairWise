package taskqueue125

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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongiddd", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructureFirst(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before state reads:
	// an invalid op later in the batch must mask an earlier ErrExists.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind(7), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed batch must not advance time.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
	if _, e := q.Pop(5, 1); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r0, e := q.Apply(Batch{Ops: nil})
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1, e)
	}
	// Failed batch rolls back revision allocation.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 0, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r2, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 0, 0}}})
	if e != nil || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2, e)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	for _, it := range s.Items {
		want := map[string]uint64{"a": 1, "b": 2, "c": 3}[it.ID]
		if it.Revision != want {
			t.Fatal(it)
		}
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	// Cancel then two enqueues: net fits mid-batch but ends over capacity.
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 0, 0}, {Enqueue, "d", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != b.Now || s.Generation != b.Generation || s.NextRevision != b.NextRevision || len(s.Items) != 2 {
		t.Fatal(s)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 3, 9},
		{Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// now=2: only c,d ready; priority tie -> ReadyAt tie -> ID asc.
	x, e := q.Pop(2, 10)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "d" {
		t.Fatal(e, x)
	}
	// Popped items are gone.
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot())
	}
	// Empty pop is fine.
	x, e = q.Pop(2, 1)
	if e != nil || len(x) != 0 {
		t.Fatal(e, x)
	}
	if _, e = q.Pop(9, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	x, e = q.Pop(9, 1)
	if e != nil || len(x) != 1 || x[0].ID != "b" {
		t.Fatal(e, x)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "yy"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
	x, _ := q.Pop(0, 1)
	x[0].ID = "zz"
	if got := q.Snapshot().Items; len(got) != 0 {
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
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := make(map[string]bool, len(s.Items))
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
	}
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}
