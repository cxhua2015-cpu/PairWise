package notificationqueue

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidIDs(t *testing.T) {
	q := queue(t)
	for _, id := range []string{"", "A", "a b", "a/b", "toolongid", "é"} {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegativeReadyAt(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(3), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	before := q.Snapshot()
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := q.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	if after.Generation != r.Generation || after.Now != 1 {
		t.Fatalf("time/generation not rolled back: %+v", after)
	}
	// Revision was rolled back too: the next enqueue reuses it.
	r2, e := q.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if e != nil || r2.Revision != r.Revision+1 {
		t.Fatal(e, r2)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r2, e := q.Apply(Batch{Now: 3})
	if e != nil || r2.Generation != r1.Generation {
		t.Fatal(e, r1, r2)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
}

func TestPopOrderAndAtomicRemoval(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 3, 1},
		{Enqueue, "c", 3, 0},
		{Enqueue, "d", 3, 0},
		{Enqueue, "e", 2, 0},
		{Enqueue, "f", 9, 100}, // not ready
	}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	got := []string{x[0].ID, x[1].ID, x[2].ID}
	if !reflect.DeepEqual(got, []string{"c", "d", "b"}) {
		t.Fatal(got)
	}
	if len(q.Snapshot().Items) != 3 {
		t.Fatal("pop must remove atomically")
	}
	x, e = q.Pop(1, 10)
	if e != nil || len(x) != 2 || x[0].ID != "e" || x[1].ID != "a" {
		t.Fatal(e, x)
	}
	if x, e = q.Pop(1, 1); e != nil || len(x) != 0 {
		t.Fatal(e, x)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "b"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal("snapshot shares state with the queue")
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
			id := fmt.Sprintf("id-%d", i)
			_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(0, 1)
			_ = q.Snapshot()
			_, _ = q.Apply(Batch{Ops: []Op{{Cancel, id, 0, 0}}})
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id survived")
		}
		seen[it.ID] = true
	}
}
