package readyqueue330

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid9"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c-9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExistsCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(e, r1)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Capacity checked only at the end: cancel-then-enqueue fits.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 3, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Over capacity at end rolls back everything, including revision.
	before := q.Snapshot()
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}, {Enqueue, "f", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := q.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Items) != len(before.Items) {
		t.Fatal(before, after)
	}
}

func TestRevisionMonotonicAndGeneration(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}}})
	if r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r)
	}
	g := q.Snapshot().Generation
	r, e := q.Apply(Batch{Now: 0})
	if e != nil || r.Generation != g || q.Snapshot().Generation != g {
		t.Fatal(e, r)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 9, 10},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(1, 4)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "d" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(5, 4)
	if len(x) != 1 || x[0].ID != "a" {
		t.Fatal(x)
	}
	x, _ = q.Pop(10, 4)
	if len(x) != 1 || x[0].ID != "b" {
		t.Fatal(x)
	}
	if got := len(q.Snapshot().Items); got != 0 {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 512, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("id-%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 512 {
		t.Fatal(len(s.Items))
	}
}
