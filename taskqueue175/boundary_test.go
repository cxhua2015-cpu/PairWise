package taskqueue175

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
			t.Fatal(o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "é", "toolongid", "a/b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	q := queue(t)
	// Unknown kind must fail even if the ID would also collide.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Negative ReadyAt / Now are structural errors.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
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
	// Failed batch must not move time backwards check baseline.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestDuplicateAndCancel(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Cancel then re-enqueue within one batch is legal.
	r, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 9, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestRevisionRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Fails on the second op; revision of "b" must be rolled back.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if q.Snapshot().NextRevision != 3 {
		t.Fatal(q.Snapshot())
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestGenerationSemantics(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	r, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "zz", 0, 0}}})
	if g := q.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
		{Enqueue, "e", 9, 9},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(1, 10)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b", "a"}) {
		t.Fatal(ids)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	// "e" is not ready until now >= 9.
	got, _ = q.Pop(8, 1)
	if len(got) != 0 {
		t.Fatal(got)
	}
	got, _ = q.Pop(9, 1)
	if len(got) != 1 || got[0].ID != "e" {
		t.Fatal(got)
	}
}

func TestPopInvalidLimit(t *testing.T) {
	q := queue(t)
	for _, l := range []int{0, -2} {
		if _, e := q.Pop(0, l); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(l, e)
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "hack"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 4096, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i % 7, int64(i % 3)}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 4096 {
		t.Fatal(len(s.Items))
	}
	// Revisions must be unique and below NextRevision.
	seen := map[uint64]bool{}
	for _, it := range s.Items {
		if seen[it.Revision] || it.Revision == 0 || it.Revision >= s.NextRevision {
			t.Fatal(it)
		}
		seen[it.Revision] = true
	}
}

func TestConcurrentCancelEnqueue(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 16; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("task-%d", g%4)
			for i := 0; i < 100; i++ {
				_, _ = q.Apply(Batch{Ops: []Op{{Cancel, id, 0, 0}, {Enqueue, id, i, 0}}})
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 4 {
		t.Fatal(n)
	}
}
