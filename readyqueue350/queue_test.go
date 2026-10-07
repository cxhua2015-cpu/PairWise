package readyqueue350

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {0, 0}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts=%+v err=%v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id=%q err=%v", id, e)
		}
	}
	good := []string{"a", "z-0_", "12345678", "a-b_c-9"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("id=%q err=%v", id, e)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	q := queue(t)
	// Unknown kind must fail even when an earlier op would hit state errors.
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}, {Kind(99), "x", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Cancel with extra fields is invalid.
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "x", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "x", 0, 1}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Negative ReadyAt / Now.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "x", 0, -1}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != b.Now || got.Generation != b.Generation {
		t.Fatalf("time rolled: %+v vs %+v", got, b)
	}
	// Equal now is allowed.
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 1 {
		t.Fatal(s)
	}
	r, e = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestRevisionAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	// Exceeds final capacity: everything must roll back.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.NextRevision != b.NextRevision || got.Generation != b.Generation || len(got.Items) != 1 {
		t.Fatalf("no rollback: %+v", got)
	}
	// Next enqueue reuses the rolled-back revision.
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Duplicate ID.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "later", 9, 10},
		{Enqueue, "p1a", 1, 0},
		{Enqueue, "p2b", 2, 1},
		{Enqueue, "p2a", 2, 0},
		{Enqueue, "p2c", 2, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(5, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"p2a", "p2c", "p2b", "p1a"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop not atomic delete")
	}
	if _, e = q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "x"})
	got, _ := q.Pop(0, 1)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
	got[0].ID = "mut"
	if q.Snapshot().Items == nil || len(q.Snapshot().Items) != 0 {
		t.Fatal("internal state leaked")
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
				id := fmt.Sprintf("g%d-i%d", g, i)
				if _, e := q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}}); e != nil {
					continue
				}
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
			}
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
	if len(s.Items) > 256 {
		t.Fatal("capacity exceeded")
	}
}
