package readyqueue265

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

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文"}
	for _, id := range bad {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z9-_"} {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndCancelPayload(t *testing.T) {
	q := queue(t)
	if e := q.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Cancel, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Cancel, "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestFailureRollsBackRevisionAndTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	before := q.Stats()
	// Fails on duplicate enqueue after a successful first op.
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	after := q.Stats()
	if after != before {
		t.Fatalf("rollback mismatch: %+v vs %+v", before, after)
	}
	// Revision counter must not leak: next enqueue gets revision 2.
	r, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r2, e := q.Apply(Batch{Ops: nil})
	if e != nil || r2.Generation != r1.Generation {
		t.Fatal(r1, r2, e)
	}
	if q.Stats().Generation != 1 {
		t.Fatal(q.Stats())
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 3, 2},
		{Enqueue, "c", 3, 1},
		{Enqueue, "d", 3, 1},
		{Enqueue, "e", 2, 0},
	}})
	x, e := q.Pop(2, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "b", "e"} // a not ready yet
	if len(x) != len(want) {
		t.Fatal(x)
	}
	for i, id := range want {
		if x[i].ID != id {
			t.Fatalf("pos %d: got %s want %s", i, x[i].ID, id)
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop must remove atomically")
	}
}

func TestPopInvalidLimit(t *testing.T) {
	q := queue(t)
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 7, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone must preserve logical clocks")
	}
	// Mutating the clone leaves the original untouched and vice versa.
	_, _ = c.Apply(Batch{Now: 8, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if q.Stats().Items != 1 || c.Stats().Items != 2 {
		t.Fatal("clone aliases original")
	}
	if _, e = q.Apply(Batch{Now: 8, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
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
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			_ = q.Stats()
			_, _ = q.Clone()
			_ = q.Snapshot()
			_, _ = q.Pop(int64(i), 1)
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
	if got := len(q.Snapshot().Items); got != s.Items {
		t.Fatalf("snapshot/stats disagree: %d vs %d", got, s.Items)
	}
}

func TestConcurrentCancelEnqueueSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 1, 0}}})
		}()
	}
	w.Wait()
	if n := q.Stats().Items; n != 1 {
		t.Fatal(n)
	}
}
