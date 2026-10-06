package readyqueue225

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsBoundary(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestValidationBoundary(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 0, 0}}},
		{Ops: []Op{{Enqueue, "A", 0, 0}}},
		{Ops: []Op{{Enqueue, "a b", 0, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 0, 0}}},
		{Ops: []Op{{Enqueue, "a", 0, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	good := Batch{Ops: []Op{{Enqueue, "a-z_09", 7, 5}, {Cancel, "a-z_09", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	// Validation is side-effect free.
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
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
	if q.Snapshot().Now != 5 {
		t.Fatal("failed batch moved time")
	}
}

func TestFailureRollsBackRevision(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	before := q.Stats()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	after := q.Stats()
	if after != before {
		t.Fatalf("rollback mismatch: %+v vs %+v", before, after)
	}
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if r.Revision != 2 {
		t.Fatalf("revision leaked: %d", r.Revision)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatalf("capacity failure leaked items: %d", n)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestPopBoundary(t *testing.T) {
	q := queue(t)
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 5}, {Enqueue, "b", 1, 0}}})
	x, e := q.Pop(0, 10)
	if e != nil || len(x) != 1 || x[0].ID != "b" {
		t.Fatalf("not-ready item popped: %v %v", x, e)
	}
	x, _ = q.Pop(5, 10)
	if len(x) != 1 || x[0].ID != "a" {
		t.Fatalf("ready item missing: %v", x)
	}
}

func TestCloneIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverged at copy time")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 9, 0}}})
	if q.Stats().Items != 1 || q.Stats().Now != 2 {
		t.Fatal("clone mutation leaked into original")
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
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Stats()
			_ = q.Snapshot()
			_, _ = q.Clone()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 || s.NextRevision < 1 {
		t.Fatalf("inconsistent stats: %+v", s)
	}
}
