package taskqueue095

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	q := queue(t)
	bad := []Op{
		{Kind: 0, ID: "a"},
		{Kind: 99, ID: "a"},
		{Kind: Enqueue, ID: ""},
		{Kind: Enqueue, ID: "A"},
		{Kind: Enqueue, ID: "a b"},
		{Kind: Enqueue, ID: "toolongid"},
		{Kind: Enqueue, ID: "a", ReadyAt: -1},
		{Kind: Cancel, ID: "é"},
	}
	for _, op := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(op, e)
		}
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestBatchValidatedBeforeState(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Second op is structurally invalid; must fail with ErrInvalidInput,
	// not ErrExists from the first op, and leave state untouched.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind: 42, ID: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := len(q.Snapshot().Items); got != 1 {
		t.Fatal(got)
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
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestExistsAndGeneration(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Empty batch: generation unchanged, time advances.
	r, _ = q.Apply(Batch{Now: 2})
	if r.Generation != 1 || q.Snapshot().Now != 2 {
		t.Fatal(r, q.Snapshot())
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestPopOrderAndDelete(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	x, e := q.Pop(0, 10)
	if e != nil || len(x) != 3 || x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatal(e, x)
	}
	if got := len(q.Snapshot().Items); got != 1 {
		t.Fatal(got)
	}
	// "b" not ready until now=1.
	x, _ = q.Pop(1, 1)
	if len(x) != 1 || x[0].ID != "b" {
		t.Fatal(x)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0] = Item{ID: "zz"}
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 100000, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 200; j++ {
				id := fmt.Sprintf("w%d-%d", i, j)
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, 0}}})
				_, _ = q.Pop(int64(j), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
