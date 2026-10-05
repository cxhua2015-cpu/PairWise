package taskqueue195

import (
	"errors"
	"fmt"
	"reflect"
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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongiddd", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "task-1", "a_b-c9", "12345678"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := q.Apply(Batch{Ops: []Op{{Kind(k), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
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
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed calls must not move time backwards/forwards.
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestRevisionRollback(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r1.Revision != 1 {
		t.Fatal(r1)
	}
	// Failing batch allocates nothing.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if r2.Revision != 2 {
		t.Fatal(r2)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	before := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Cancel-then-enqueue within one batch fits because capacity is final-only.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExistsAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Empty batch: generation unchanged.
	r2, e := q.Apply(Batch{Now: 1})
	if e != nil || r2.Generation != r1.Generation {
		t.Fatal(r2, e)
	}
	// Non-empty batch bumps generation exactly once even with many ops.
	r3, _ := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if r3.Generation != r1.Generation+1 {
		t.Fatal(r3)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "p1a", 1, 0},
		{Enqueue, "p2", 2, 5},
		{Enqueue, "p1b", 1, 0},
	}})
	got, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"p2", "p1a", "p1b"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	// Pop atomically removed the items; only the not-ready one remains.
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "late" {
		t.Fatal(s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "zz"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal(got)
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "mut"
	if got := q.Snapshot(); len(got.Items) != 0 {
		t.Fatal(got)
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
				id := fmt.Sprintf("g%d-task%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 256 {
		t.Fatal(n)
	}
}
