package readyqueue290

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDRules(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "abcdefgh"} {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind(0), "a", 0, 0}}},
		{Ops: []Op{{Kind(99), "a", 0, 0}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
		{Ops: []Op{{Enqueue, "a", 0, -1}}},
	}
	for i, b := range cases {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("failed validation mutated state: %+v", s)
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
	if q.Snapshot().Now != 5 {
		t.Fatal("time rolled back incorrectly")
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal("equal time must be accepted:", e)
	}
}

func TestExistsAndRevisionRollback(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r1.Revision != 1 || r1.Generation != 1 {
		t.Fatalf("%+v", r1)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.NextRevision != b.NextRevision || s.Generation != b.Generation || len(s.Items) != 1 {
		t.Fatalf("revision/state not rolled back: %+v", s)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if r2.Revision != 2 {
		t.Fatalf("revision reused after rollback: %+v", r2)
	}
}

func TestCapacityRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "e", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Generation != b.Generation || s.NextRevision != b.NextRevision || len(s.Items) != 4 {
		t.Fatalf("capacity failure leaked state: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if g := q.Stats().Generation; g != 1 {
		t.Fatal("non-empty batch must bump generation exactly once, got", g)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	got, e := q.Pop(0, 10)
	if e != nil || len(got) != 3 || got[0].ID != "c" || got[1].ID != "d" || got[2].ID != "a" {
		t.Fatal(e, got)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal("pop not atomic:", n)
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
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverges at copy time")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}}})
	if q.Stats().Now != 2 || len(q.Snapshot().Items) != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if len(c.Snapshot().Items) != 1 {
		t.Fatal("clone lost its own updates")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Stats()
			_ = q.Snapshot()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
			if i%8 == 0 {
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items != len(q.Snapshot().Items) {
		t.Fatal("stats/snapshot mismatch")
	}
}
