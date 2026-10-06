package readyqueue295

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {-1, 8}, {4, 0}, {4, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},
		{Ops: []Op{{Enqueue, "bad id", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Kind(0), "a", 0, 0}}},
		{Ops: []Op{{Kind(99), "a", 0, 0}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range cases {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if e := q.ValidateBatch(Batch{Now: 5, Ops: []Op{{Enqueue, "ok_id-1", 0, 0}, {Cancel, "ok_id-1", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Validation is side-effect free: state must be untouched.
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 0 || len(s.Items) != 0 {
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
	// Failed calls must not move the clock; equal-now is allowed.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	r, _ = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestRevisionRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// b's revision was rolled back: next enqueue reuses revision 2.
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
	if q.Snapshot().NextRevision != 3 {
		t.Fatal(q.Snapshot())
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Items) != 2 {
		t.Fatalf("no rollback: %+v", got)
	}
	// Cancel-then-enqueue within one batch fits the final capacity.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Cancel, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "p1", 1, 0},
		{Enqueue, "p3b", 3, 1},
		{Enqueue, "p3a", 3, 1},
		{Enqueue, "p3c", 3, 2},
		{Enqueue, "future", 9, 100},
	}})
	got, e := q.Pop(1, 10)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	want := []string{"p3a", "p3b", "p1"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 2 {
		t.Fatal(n)
	}
	// Limit truncates in canonical order.
	got, _ = q.Pop(100, 1)
	if len(got) != 1 || got[0].ID != "future" {
		t.Fatal(got)
	}
}

func TestPopInvalidInput(t *testing.T) {
	q := queue(t)
	for _, args := range [][2]int64{{-1, 1}, {0, 0}, {0, -2}} {
		if _, e := q.Pop(args[0], int(args[1])); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(args, e)
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "x"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatalf("snapshot aliases state: %+v", got)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 7, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	a, b := q.Stats(), c.Stats()
	if a != b {
		t.Fatalf("clocks differ: %+v %+v", a, b)
	}
	// Mutating the clone must not affect the original and vice versa.
	_, _ = c.Apply(Batch{Now: 8, Ops: []Op{{Enqueue, "b", 1, 0}}})
	_, _ = q.Pop(7, 1)
	if q.Stats().Items != 0 || c.Stats().Items != 2 {
		t.Fatal("clone aliases original")
	}
}

func TestConcurrentMix(t *testing.T) {
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
				_ = q.ValidateBatch(Batch{Ops: []Op{{Cancel, id, 0, 0}}})
				_ = q.Stats()
				_ = q.Snapshot()
				if i%7 == 0 {
					_, _ = q.Pop(int64(i), 3)
				}
				if i%11 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if int(s.Items) != len(q.Snapshot().Items) {
		t.Fatal("stats/snapshot mismatch")
	}
	if s.NextRevision < s.Generation {
		t.Fatal("revision clock behind generation")
	}
}
