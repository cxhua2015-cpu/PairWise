package readyqueue410

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

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 3, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "bad id", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},
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
	if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok_id-1", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
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
	if s := q.Stats(); s.Now != 5 || s.Items != 1 {
		t.Fatalf("failed ops must roll back time: %+v", s)
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
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision ||
		after.Now != before.Now || len(after.Items) != len(before.Items) {
		t.Fatalf("capacity failure must roll back: %+v vs %+v", before, after)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 3, 1},
		{Enqueue, "c", 3, 0},
		{Enqueue, "d", 3, 0},
	}})
	x, e := q.Pop(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "b"}
	for i, it := range x {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", x, want)
		}
	}
	if s := q.Stats(); s.Items != 1 {
		t.Fatal(s)
	}
	if _, e := q.Pop(1, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Stats(); s.Generation != 0 || s.Now != 0 {
		t.Fatalf("empty batch must not change state: %+v", s)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if s := c.Stats(); s.Now != 2 || s.Generation != 1 || s.NextRevision != 2 || s.Items != 1 {
		t.Fatalf("clone must preserve clocks: %+v", s)
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}})
	if q.Stats().Items != 1 || c.Stats().Items != 0 {
		t.Fatal("clone shares state with original")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "zz"})
	if q.Stats().Items != 1 || q.Snapshot().Items[0].ID != "a" {
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
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 4)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
			}
		}()
	}
	w.Wait()
	if _, e := q.Clone(); e != nil {
		t.Fatal(e)
	}
}
