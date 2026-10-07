package readyqueue335

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "abcdefgh"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructureFirst(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural failure must not consume revisions or change state even
	// when earlier ops in the same batch are valid.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind: 9, ID: "b"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
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

func TestDuplicateAndCancel(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("rollback failed")
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	a := q.Snapshot()
	if a.Generation != b.Generation || a.NextRevision != b.NextRevision || len(a.Items) != 1 {
		t.Fatalf("no rollback: %+v vs %+v", a, b)
	}
	// Revision counter must not leak after a failed batch.
	r, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("time not advanced")
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "b", 1, 0},
		{Enqueue, "a", 1, 0},
		{Enqueue, "c", 2, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(0, 8)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "a", "b"}
	if len(x) != len(want) {
		t.Fatal(x)
	}
	for i, id := range want {
		if x[i].ID != id {
			t.Fatal(x)
		}
	}
	if _, e = q.Pop(99, 1); e != nil {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
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
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id")
		}
		seen[it.ID] = true
	}
	if len(s.Items) > 512 {
		t.Fatal("capacity exceeded")
	}
}
