package readyqueue390

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "ä", "toolongiddd", "UPPER"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Failed batch must not advance time.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
	// Equal time is allowed.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Duplicate within one batch.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 0, 0}, {Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Cancel of ID enqueued earlier in the same batch is allowed.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 0, 0}, {Cancel, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "c", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestRollbackRestoresRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 1 {
		t.Fatal(r1, e)
	}
	b := q.Snapshot()
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// Revision counter must not leak from the failed batch.
	r2, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r2.Revision != 2 || r2.Generation != 2 {
		t.Fatal(r2, e)
	}
}

func TestEmptyBatchNoOp(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Generation != 0 || s.Now != 0 || len(s.Items) != 0 {
		t.Fatal(s)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Transient overflow within the batch is fine; only the final size counts.
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}, {Enqueue, "c", 0, 0}, {Cancel, "c", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 0, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal("capacity failure must roll back")
	}
}

func TestPopOrderingAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "low", 1, 0},
		{Enqueue, "future", 9, 100},
		{Enqueue, "b", 5, 2},
		{Enqueue, "a", 5, 2},
		{Enqueue, "early", 5, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(50, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"early", "a", "b", "low"}
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatal(ids)
	}
	// Pop is atomic removal.
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot().Items)
	}
	later, e := q.Pop(100, 1)
	if e != nil || len(later) != 1 || later[0].ID != "future" {
		t.Fatal(later, e)
	}
	// Limit larger than ready set and zero limit.
	empty, e := q.Pop(100, 0)
	if e != nil || len(empty) != 0 {
		t.Fatal(empty, e)
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
				id := fmt.Sprintf("g%d-%d", g, i)
				now := int64(i)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(now, 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
