package readyqueue280

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "ABC", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},
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
	good := Batch{Ops: []Op{{Enqueue, "ok_id-1", 5, 3}, {Cancel, "ok_id-1", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	// ValidateBatch must be side-effect free.
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("validate mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
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
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatalf("failed batch changed state: %+v", s)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatalf("empty batch changed state: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	before := q.Snapshot()
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := q.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Items) != 2 {
		t.Fatalf("capacity failure not rolled back: %+v", after)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 9, 9},
		{Enqueue, "c", 1, 2},
		{Enqueue, "d", 1, 2},
	}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(5, 10)
	if e != nil || len(x) != 3 {
		t.Fatal(e, x)
	}
	if x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatalf("order: %v", x)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop did not delete atomically")
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r1, r2)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatalf("%+v", s)
	}
	for _, it := range s.Items {
		if it.ID == "c" && it.Revision != 3 {
			t.Fatalf("item revision: %+v", it)
		}
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if got := c.Stats(); got.Now != 2 || got.Generation != 1 || got.NextRevision != 2 || got.Items != 1 {
		t.Fatalf("clone clocks: %+v", got)
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}})
	_, _ = q.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}}})
	if q.Stats().Items != 0 || c.Stats().Items != 2 {
		t.Fatal("clone aliases original")
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
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Stats()
				_ = q.Snapshot()
				if i%10 == 0 {
					_, _ = q.Clone()
				}
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("stats: %+v", s)
	}
	if got := len(q.Snapshot().Items); got != s.Items {
		t.Fatalf("snapshot=%d stats=%d", got, s.Items)
	}
}
