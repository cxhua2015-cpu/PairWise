package readyqueue220

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatal(id, e)
		}
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "ok_id-1", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndFullValidation(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(9), "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural failure anywhere must leave state untouched (a not enqueued).
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
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
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	// Capacity checked only at the end: cancel+2 enqueues within limit 2 ok.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Items) != 2 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r0, e := q.Apply(Batch{Now: 0}) // empty batch: no generation bump
	if e != nil || r0.Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0}, {Enqueue, "d", 9, 9},
	}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(1, 2)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "b" {
		t.Fatal(e, x)
	}
	if _, e := q.Pop(1, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// d not ready until 9.
	x, e = q.Pop(8, 4)
	if e != nil || len(x) != 1 || x[0].ID != "a" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(9, 4)
	if len(x) != 1 || x[0].ID != "d" {
		t.Fatal(x)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
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
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%d", i)
			for j := 0; j < 20; j++ {
				now := int64(j + 1)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(now, 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
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
}
