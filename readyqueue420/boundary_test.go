package readyqueue420

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "ABC", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range cases {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	ok := Batch{Now: 1, Ops: []Op{{Enqueue, "a-z_09", 1, 0}, {Cancel, "a-z_09", 0, 0}}}
	if e := q.ValidateBatch(ok); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Generation != 0 || s.Items != 0 || s.Now != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Now != 5 {
		t.Fatalf("time rolled back incorrectly: %+v", s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || q.Stats().Now != 3 || q.Stats().Generation != 0 {
		t.Fatal(r, e, q.Stats())
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 0 || s.Generation != 1 || s.NextRevision != 3 || len(s.Items) != 2 {
		t.Fatalf("no rollback: %+v", s)
	}
	_ = b
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 1 || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r1, r2)
	}
	items := q.Snapshot().Items
	if items[0].Revision != 1 || items[1].Revision != 2 || items[2].Revision != 3 {
		t.Fatal(items)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 9, 5},
		{Enqueue, "z", 9, 1},
	}})
	got, e := q.Pop(4, 10)
	if e != nil || len(got) != 1 || got[0].ID != "z" {
		t.Fatal(e, got)
	}
	got, _ = q.Pop(5, 10)
	if len(got) != 2 || got[0].ID != "y" || got[1].ID != "x" {
		t.Fatal(got)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if q.Stats().Now != 2 || q.Stats().Items != 1 || q.Stats().Generation != 1 {
		t.Fatalf("original affected: %+v", q.Stats())
	}
	if c.Stats().Now != 3 || c.Stats().Items != 2 {
		t.Fatalf("clone not advanced: %+v", c.Stats())
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
			id := fmt.Sprintf("task-%d", i)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Stats()
			_ = q.Snapshot()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
			if i%4 == 0 {
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 32 {
		t.Fatalf("bad stats: %+v", s)
	}
}
