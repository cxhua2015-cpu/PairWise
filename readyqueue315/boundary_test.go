package readyqueue315

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {0, 0}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid9", "a.b", "+x"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "ok_id-9", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndAtomicValidation(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before state reads:
	// an invalid kind later in the batch must win over ErrExists earlier.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(9), "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("batch was not atomic")
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
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Net size stays within capacity even though the batch peaks above it.
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeding capacity rolls back the entire batch including revisions.
	before := q.Snapshot()
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "x", 1, 0}, {Enqueue, "y", 1, 0}, {Enqueue, "z", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if after := q.Snapshot(); after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Items) != len(before.Items) {
		t.Fatal("rollback failed", before, after)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "future", 99, 100},
		{Enqueue, "b", 2, 2},
		{Enqueue, "a", 2, 2},
		{Enqueue, "c", 3, 5},
		{Enqueue, "d", 3, 1},
	}})
	got, e := q.Pop(10, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"d", "c", "a"}
	if len(got) != 3 {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	if got[0].Revision == 0 {
		t.Fatal("revision not assigned")
	}
	if n := len(q.Snapshot().Items); n != 2 {
		t.Fatal(n)
	}
	// "future" is not ready yet.
	got, _ = q.Pop(10, 5)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got)
	}
	if _, e := q.Pop(10, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "zz"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
}

func TestConcurrentApplyPop(t *testing.T) {
	q, _ := New(Options{MaxItems: 1000, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i)}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 1000 {
		t.Fatal(len(s.Items))
	}
	seen := map[string]bool{}
	var prevRev uint64
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
		if it.Revision == 0 || it.Revision >= s.NextRevision {
			t.Fatal("bad revision", it)
		}
		if it.Revision > prevRev {
			prevRev = it.Revision
		}
	}
}
