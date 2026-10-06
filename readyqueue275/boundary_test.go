package readyqueue275

import (
	"errors"
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

func TestIDRules(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-9_", "ab-12_x"} {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndCancelPayload(t *testing.T) {
	q := queue(t)
	if e := q.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Cancel, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Cancel, "a", 0, 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	before := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Failed batches must not advance time, state or revision.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision ||
		got.Generation != before.Generation || len(got.Items) != len(before.Items) {
		t.Fatalf("state changed after failures: %+v vs %+v", got, before)
	}
}

func TestCapacityCheckedLast(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Cancel-then-enqueue within one batch fits because capacity is final-only.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Net growth beyond capacity fails and rolls everything back.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 3 {
		t.Fatalf("%+v", s)
	}
	r, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestPopOrdering(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 5, 9},
		{Enqueue, "z", 5, 2},
		{Enqueue, "w", 5, 2},
	}})
	got, e := q.Pop(10, 3)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	// Same priority: ReadyAt asc, then ID asc.
	if got[0].ID != "w" || got[1].ID != "z" || got[2].ID != "y" {
		t.Fatal(got)
	}
	if got[0].Revision == 0 {
		t.Fatal("revision missing")
	}
	// Not-yet-ready item stays queued.
	if s := q.Snapshot(); len(s.Items) != 1 || s.Items[0].ID != "x" {
		t.Fatalf("%+v", s)
	}
	if _, e := q.Pop(1, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 2 || len(c.Snapshot().Items) != 1 {
		t.Fatal("clone aliases original")
	}
	// Clone preserves logical clocks: revisions continue, not restart.
	r, e := c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
}

func TestStatsLinearizable(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i)) + "-x"
			_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items != 8 || s.NextRevision != 9 {
		t.Fatalf("%+v", s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i)) + "-id"
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Snapshot()
			_ = q.Stats()
			_, _ = q.Clone()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "zz", 1, 0}}})
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 16 {
		t.Fatalf("%+v", s)
	}
	if got := len(q.Snapshot().Items); got != s.Items {
		t.Fatal(got, s.Items)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s1 := q.Snapshot()
	s1.Items[0].Priority = 99
	if q.Snapshot().Items[0].Priority != 1 {
		t.Fatal("snapshot aliases state")
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "mut"
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("pop leaked state")
	}
}
