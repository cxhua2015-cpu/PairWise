package readyqueue280

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndStructuralBoundaries(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 8}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}}, // > MaxIDBytes=8
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok-id_1", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestMonotonicTimeAndEmptyBatch(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	before := q.Snapshot()
	r, e := q.Apply(Batch{Now: 9})
	if e != nil || r.Generation != before.Generation {
		t.Fatal(r, e)
	}
	after := q.Snapshot()
	if after.Generation != before.Generation || after.Now != before.Now {
		t.Fatalf("empty batch mutated state: %+v -> %+v", before, after)
	}
}

func TestFailedBatchRollsBackRevision(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Fails at the end on capacity; revision must not leak.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "p1", 1, 0},
		{Enqueue, "p2a", 2, 5},
		{Enqueue, "p2b", 2, 3},
	}})
	got, e := q.Pop(10, 3)
	if e != nil || len(got) != 3 || got[0].ID != "p2b" || got[1].ID != "p2a" || got[2].ID != "p1" {
		t.Fatal(e, got)
	}
	if s := q.Snapshot(); len(s.Items) != 1 || s.Items[0].ID != "late" {
		t.Fatal(s)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	z := c.Stats()
	if z.Now != 3 || z.Generation != 1 || z.NextRevision != 2 || z.Items != 1 {
		t.Fatal(z)
	}
	// Clone continues revisions where the original left off.
	r, e := c.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if q.Stats().Items != 2 || c.Stats().Items != 2 {
		t.Fatal("state aliased")
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
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
}
