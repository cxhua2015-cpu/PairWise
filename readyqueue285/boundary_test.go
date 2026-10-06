package readyqueue285

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestValidationBoundaries(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind(0), "a", 0, 0}}},
		{Ops: []Op{{Kind(99), "a", 0, 0}}},
		{Ops: []Op{{Enqueue, "", 0, 0}}},
		{Ops: []Op{{Enqueue, "A", 0, 0}}},
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
	}
	good := Batch{Now: 3, Ops: []Op{{Enqueue, "ok_id-1", 5, 0}, {Cancel, "ok_id-1", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	// Validation must be side-effect free.
	if s := q.Snapshot(); s.Now != 0 || s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
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
	if _, e := q.Pop(-1, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{})
	if r.Generation != 1 || r.Revision != 0 {
		t.Fatalf("empty batch changed counters: %+v", r)
	}
	z := q.Stats()
	if z.Generation != 1 || z.NextRevision != 3 || z.Items != 2 {
		t.Fatalf("stats: %+v", z)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0}, {Enqueue, "d", 9, 9},
	}})
	x, e := q.Pop(1, 10)
	if e != nil || len(x) != 3 || x[0].ID != "c" || x[1].ID != "b" || x[2].ID != "a" {
		t.Fatal(e, x)
	}
	if x, _ = q.Pop(1, 10); len(x) != 0 {
		t.Fatal(x)
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
	if len(q.Snapshot().Items) != 1 || len(c.Snapshot().Items) != 2 {
		t.Fatal("clone not independent")
	}
	if q.Snapshot().Now != 2 || c.Snapshot().Now != 3 {
		t.Fatal("clocks not isolated")
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
				_, _ = q.Pop(int64(i), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "x", 0, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	z := q.Stats()
	if z.Items < 0 || z.Items > 256 {
		t.Fatal(z)
	}
}
