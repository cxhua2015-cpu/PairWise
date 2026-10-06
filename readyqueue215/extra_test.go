package readyqueue215

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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongiddd", "a.b"}
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

func TestUnknownKindAndNegativeReadyAt(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
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
	// Failed batch must not advance time or state.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty batch: generation unchanged.
	r, e = q.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Cancel-only batch: generation bumps, no new revision.
	r, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 3 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	// Failed batch rolls back revision counter.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 0, 0}, {Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 0, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	// Capacity checked only at the end: cancel frees room mid-batch.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Exceeding capacity rolls back the whole batch including time.
	now := q.Snapshot().Now
	_, e := q.Apply(Batch{Now: now + 10, Ops: []Op{{Enqueue, "e", 0, 0}, {Enqueue, "f", 0, 0}, {Enqueue, "g", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != now {
		t.Fatal("time not rolled back")
	}
	_ = b
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 3, 9},
		{Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Only ready items are eligible; ties break by ReadyAt then ID.
	x, e := q.Pop(5, 2)
	if e != nil || len(x) != 2 || x[0].ID != "c" || x[1].ID != "d" {
		t.Fatal(x, e)
	}
	// Pop atomically removes; "a" is next by priority among remaining ready.
	x, e = q.Pop(5, 10)
	if e != nil || len(x) != 1 || x[0].ID != "a" {
		t.Fatal(x, e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot().Items)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	p, _ := q.Pop(0, 1)
	p[0].ID = "zz"
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("pop result aliases internal state")
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i)}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}
