package readyqueue215

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "ok_id-1", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}, {Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestTimeMonotonic(t *testing.T) {
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

func TestDuplicateAndCancel(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r1.Revision != 1 {
		t.Fatal(e, r1)
	}
	// Capacity exceeded at end: revision must not leak.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || len(s.Items) != 1 || s.Generation != 1 {
		t.Fatalf("%+v", s)
	}
	// Cancel then enqueue beyond capacity still fails and rolls back.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("capacity failure leaked items")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "p1", 1, 0},
		{Enqueue, "p2a", 2, 1},
		{Enqueue, "p2b", 2, 0},
		{Enqueue, "future", 9, 100},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"p2b", "p2a", "p1"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	// Items are removed atomically; future one remains.
	if s := q.Snapshot(); len(s.Items) != 1 || s.Items[0].ID != "future" {
		t.Fatalf("%+v", s.Items)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot shares internal state")
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 256 {
		t.Fatal(n)
	}
}
