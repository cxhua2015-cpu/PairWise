package readyqueue260

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndStructuralValidation(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	bad := []Batch{
		{Now: -1, Ops: nil},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}},
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
	if s := q.Stats(); s.Generation != 0 || s.Items != 0 || s.NextRevision != 1 {
		t.Fatalf("validation mutated state: %+v", s)
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
	r, e = q.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal("empty batch must not change generation", r, e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("empty batch must not advance time")
	}
}

func TestCapacityRollbackRevisions(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	_, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.Now != 0 || s.Generation != 1 || s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal("revision must be reused after rollback", r, e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "z", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 1},
		{Enqueue, "z", 5, 0},
		{Enqueue, "w", 5, 0},
		{Enqueue, "future", 9, 100},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"w", "z", "y", "x"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if _, e = q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal("clone must preserve revision clock independently", e)
	}
	if q.Stats().Now != 3 || c.Stats().Items != 2 {
		t.Fatal("clone shares state with original")
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
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok", 1, 0}}})
				_ = q.Stats()
				_ = q.Snapshot()
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
