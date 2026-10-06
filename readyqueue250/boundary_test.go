package readyqueue250

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestValidationBoundaries(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "A", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}},
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
	good := Batch{Ops: []Op{{Enqueue, "a-z_09", 1, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
}

func TestMonotonicTimeAndEmptyBatch(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 1}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if _, e = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	before := q.Snapshot()
	r, e = q.Apply(Batch{Now: 9})
	if e != nil || r.Generation != before.Generation {
		t.Fatal(e, r)
	}
	if got := q.Snapshot(); got.Generation != before.Generation || got.Now != before.Now {
		t.Fatalf("empty batch mutated state: %+v", got)
	}
}

func TestRevisionRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatalf("revision leaked: %+v", s)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 2 {
		t.Fatal(n)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "b", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestPopSemantics(t *testing.T) {
	q := queue(t)
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 9, 5},
		{Enqueue, "c", 9, 1},
	}})
	x, e := q.Pop(4, 10)
	if e != nil || len(x) != 1 || x[0].ID != "c" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(5, 1)
	if len(x) != 1 || x[0].ID != "b" {
		t.Fatal(x)
	}
	s := q.Stats()
	if s.Items != 1 {
		t.Fatal(s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%02d", i)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Snapshot()
			_ = q.Stats()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			if i%4 == 0 {
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
	if got := len(q.Snapshot().Items); got != s.Items {
		t.Fatalf("stats/snapshot disagree: %d vs %d", s.Items, got)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if q.Stats().Items != 1 || q.Stats().Now != 3 {
		t.Fatal("clone mutated original")
	}
	if _, e = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}
