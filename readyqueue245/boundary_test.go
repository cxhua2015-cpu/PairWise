package readyqueue245

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndInputBoundaries(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 0, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 0, 0}}},
		{Ops: []Op{{Enqueue, "a b", 0, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 0, 0}}},
		{Ops: []Op{{Enqueue, "a", 0, -1}}},
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
	if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok_id-1", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTimeAndEmptyBatch(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if _, e = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	before := q.Snapshot()
	r, e = q.Apply(Batch{Now: 9})
	if e != nil || r.Generation != before.Generation {
		t.Fatal(r, e)
	}
	after := q.Snapshot()
	if after.Generation != before.Generation || after.Now != before.Now || after.NextRevision != before.NextRevision {
		t.Fatalf("empty batch mutated state: %+v -> %+v", before, after)
	}
}

func TestRevisionRollbackAndReuse(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatalf("revision not rolled back: %+v", s)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal("capacity failure leaked items")
	}
}

func TestPopOrderingAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Now: 10, Ops: []Op{
		{Enqueue, "x", 1, 5}, {Enqueue, "y", 1, 3}, {Enqueue, "z", 5, 20},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(10, 5)
	if e != nil || len(got) != 2 || got[0].ID != "y" || got[1].ID != "x" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(10, 1)
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 12})
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
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 128 || s.NextRevision < 1 {
		t.Fatalf("bad stats: %+v", s)
	}
	snap := q.Snapshot()
	if len(snap.Items) != s.Items || snap.Generation != s.Generation || snap.Now != s.Now {
		t.Fatal("stats and snapshot disagree")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	cs, qs := c.Stats(), q.Stats()
	if cs != qs {
		t.Fatal("clone diverges")
	}
	if _, e = c.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if q.Stats().Items != 1 || c.Stats().Items != 0 {
		t.Fatal("clone not isolated")
	}
	if q.Stats().Now != 3 || c.Stats().Now != 4 {
		t.Fatal("clocks not independent")
	}
}
