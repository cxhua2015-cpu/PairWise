package taskqueue200

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
	bad := []string{"", "A", "a b", "a.b", "é", "toolongiddd"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
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
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Now: 1})
	if r.Generation != 0 || q.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Cancel, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 0 {
		t.Fatal(r)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Now != 2 {
		t.Fatal(s)
	}
}

func TestDuplicateAndCancelMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "a" || s.NextRevision != 2 {
		t.Fatal(s)
	}
	// Cancel-then-enqueue within one batch may exceed transiently but pass final check.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "c", 0, 0}}})
	if e != nil || len(q.Snapshot().Items) != 2 {
		t.Fatal(e)
	}
}

func TestPopOrderAndFilter(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 2, 9},
		{Enqueue, "c", 2, 1},
		{Enqueue, "d", 2, 1},
	}})
	got, e := q.Pop(5, 10)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	want := []string{"c", "d", "a"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop must remove atomically")
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
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
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
}
