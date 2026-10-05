package taskqueue160

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
			t.Fatal(o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "z9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Duplicate ID would be ErrExists, but a later structurally invalid op
	// must win because validation precedes state reads.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(99), "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, -5}}})
	if !errors.Is(e, ErrInvalidInput) {
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
	// Failed batch must not advance time.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
	// Equal time is allowed.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Failed batch rolls back revision and generation.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s2 := q.Snapshot(); s2.NextRevision != 4 || s2.Generation != 2 || len(s2.Items) != 2 {
		t.Fatal(s2)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Transiently exceeds capacity within the batch, fine at the end.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Exceeds at the end: full rollback.
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopSemantics(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 2},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1},
		{Enqueue, "e", 9, 100},
	}})
	if _, e := q.Pop(1, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	x, e := q.Pop(1, 10)
	if e != nil || len(x) != 3 {
		t.Fatal(e, x)
	}
	// Priority desc, ReadyAt asc, ID asc.
	if x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatal(x)
	}
	// Pop atomically removes.
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot().Items)
	}
	// Returned slice is isolated from internal state.
	x[0].ID = "mutated"
	if q.Snapshot().Items[0].ID == "mutated" {
		t.Fatal("snapshot aliasing")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].Priority = 999
	if q.Snapshot().Items[0].Priority != 1 {
		t.Fatal("snapshot aliases internal state")
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
				id := fmt.Sprintf("g%d-%04d", g, i)
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
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
	if s.Generation == 0 || s.NextRevision < 1 {
		t.Fatal(s)
	}
}

func TestConcurrentSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	var w sync.WaitGroup
	var mu sync.Mutex
	exists, notFound := 0, 0
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
			mu.Lock()
			if errors.Is(e, ErrExists) {
				exists++
			}
			mu.Unlock()
		}()
	}
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, e := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}})
			mu.Lock()
			if errors.Is(e, ErrNotFound) {
				notFound++
			}
			mu.Unlock()
		}()
	}
	w.Wait()
	if exists != 15 || notFound != 8 {
		t.Fatal(exists, notFound)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot().Items)
	}
}
