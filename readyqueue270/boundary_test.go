package readyqueue270

import (
	"errors"
	"fmt"
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

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongid", "a+b"}
	for _, id := range bad {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "12345678"} {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
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
	// Validation is side-effect free: state must be untouched.
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 || s.NextRevision != b.NextRevision {
		t.Fatalf("failed apply mutated state: %+v", s)
	}
	// Equal time is allowed.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatalf("exists rollback left %d items", n)
	}
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("capacity rollback: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0}, {Enqueue, "d", 5, 0},
	}})
	x, e := q.Pop(0, 10)
	if e != nil || len(x) != 3 || x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatal(e, x)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if x, e = q.Pop(0, 1); e != nil || len(x) != 0 {
		t.Fatal(e, x)
	}
	if _, e := q.Pop(1, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
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
		t.Fatal(e)
	}
	qs, cs := q.Stats(), c.Stats()
	if qs != cs {
		t.Fatalf("diverged: %+v vs %+v", qs, cs)
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
				now := int64(i)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_ = q.ValidateBatch(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(now, 4)
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
	snap := q.Snapshot()
	if s.Items != len(snap.Items) || s.Generation != snap.Generation || s.NextRevision != snap.NextRevision {
		t.Fatalf("inconsistent: %+v vs %+v", s, snap)
	}
}
