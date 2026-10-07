package readyqueue395

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
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegativeReadyAt(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 1); e != nil {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestFailedBatchDoesNotAdvanceTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Failing batch at a later time must not advance the clock.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 3 {
		t.Fatal(s.Now)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// Empty batch: generation unchanged.
	r, e = q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch: generation and revisions rolled back.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s = q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 || len(s.Items) != 2 {
		t.Fatal(s)
	}
	// Next enqueue reuses the rolled-back revision.
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r.Revision != 3 || r.Generation != 2 {
		t.Fatal(r, e)
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Capacity checked only at the end; cancel-then-enqueue fits.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestPopOrderingAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "low", 1, 0},
		{Enqueue, "hi-new", 5, 2},
		{Enqueue, "hi-old", 5, 1},
		{Enqueue, "hi-b", 5, 1},
		{Enqueue, "future", 9, 100},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// "future" not ready yet.
	got, e := q.Pop(50, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"hi-b", "hi-old", "hi-new", "low"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	// Popped items are atomically removed.
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	got, e = q.Pop(100, 1)
	if e != nil || len(got) != 1 || got[0].ID != "future" {
		t.Fatal(got, e)
	}
}

func TestPopInvalidLimit(t *testing.T) {
	q := queue(t)
	for _, limit := range []int{0, -1} {
		if _, e := q.Pop(0, limit); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(e)
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 2, 0}}}); e != nil {
		t.Fatal(e)
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "injected"})
	if got := q.Snapshot(); len(got.Items) != 2 || got.Items[0].ID != "b" {
		t.Fatal(got)
	}
	p, _ := q.Pop(0, 1)
	if len(p) != 1 || p[0].ID != "b" {
		t.Fatal(p)
	}
	p[0].ID = "mutated"
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 1000, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i)}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 1000 {
		t.Fatal(len(s.Items))
	}
	seen := map[string]bool{}
	var prev *Item
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
		if prev != nil {
			if prev.Priority < it.Priority ||
				(prev.Priority == it.Priority && prev.ReadyAt > it.ReadyAt) ||
				(prev.Priority == it.Priority && prev.ReadyAt == it.ReadyAt && prev.ID > it.ID) {
				t.Fatal("snapshot not in canonical order")
			}
		}
		p := it
		prev = &p
	}
}

func TestConcurrentSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	var w sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "same", 1, 0}}})
			mu.Lock()
			if e == nil {
				wins++
			} else if !errors.Is(e, ErrExists) {
				t.Error(e)
			}
			mu.Unlock()
		}()
	}
	w.Wait()
	if wins != 1 || len(q.Snapshot().Items) != 1 {
		t.Fatal(wins)
	}
}
