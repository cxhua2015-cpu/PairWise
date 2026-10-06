package readyqueue275

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndInputBoundaries(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	bad := []Batch{
		{Now: -1},                                          // negative time
		{Ops: []Op{{Kind: 0, ID: "a"}}},                    // unknown kind
		{Ops: []Op{{Kind: 99, ID: "a"}}},                   // unknown kind
		{Ops: []Op{{Enqueue, "", 1, 0}}},                   // empty id
		{Ops: []Op{{Enqueue, "ABC", 1, 0}}},                // uppercase
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},                // space
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}},          // over 8 bytes
		{Ops: []Op{{Enqueue, "a", 1, -1}}},                 // negative ready
		{Ops: []Op{{Cancel, "a", 1, 0}}},                   // cancel with priority
		{Ops: []Op{{Cancel, "a", 0, 1}}},                   // cancel with readyat
		{Ops: []Op{{Enqueue, "ok-1_2", 1, 0}, {Kind: 7}}},  // full-batch check
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok-1_2", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndEmptyBatch(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if g := q.Snapshot().Generation; g != 0 {
		t.Fatalf("empty batch changed generation: %d", g)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityRollbackAndRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	before := q.Snapshot()
	// Cancel makes room mid-batch, but two more enqueues overflow at the end.
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Items) != 2 {
		t.Fatalf("no rollback: %+v vs %+v", got, before)
	}
	// Duplicate and missing ids also roll back.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0}, {Enqueue, "y", 5, 1}, {Enqueue, "z", 5, 0},
	}})
	got, e := q.Pop(1, 3)
	if e != nil || len(got) != 3 || got[0].ID != "z" || got[1].ID != "y" || got[2].ID != "x" {
		t.Fatal(got, e)
	}
	if got[0].Revision == 0 || len(q.Snapshot().Items) != 0 {
		t.Fatal(got)
	}
	// Mutating returned items must not affect the queue.
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "s", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "hacked"
	if q.Snapshot().Items[0].ID != "s" {
		t.Fatal("snapshot aliases state")
	}
	// Not-ready items stay queued.
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "later", 9, 100}}})
	p, _ := q.Pop(1, 10)
	if len(p) != 1 || p[0].ID != "s" || len(q.Snapshot().Items) != 1 {
		t.Fatal(p)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if len(q.Snapshot().Items) != 1 || q.Stats().Now != 2 {
		t.Fatal("clone writes leaked into original")
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
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Stats()
			_ = q.Snapshot()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
			if i%8 == 0 {
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 || s.NextRevision < 1 {
		t.Fatalf("inconsistent stats: %+v", s)
	}
}
