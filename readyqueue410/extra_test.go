package readyqueue410

import (
	"errors"
	"sync"
	"testing"
)

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := q.Snapshot()
	if got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Items) != 1 {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Stats(); s.Generation != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestPopReadinessAndOrder(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "future", 9, 10},
		{Enqueue, "low", 1, 0},
		{Enqueue, "hi2", 5, 1},
		{Enqueue, "hi1", 5, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(5, 10)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 3 || got[0].ID != "hi1" || got[1].ID != "hi2" || got[2].ID != "low" {
		t.Fatalf("%+v", got)
	}
	if s := q.Stats(); s.Items != 1 {
		t.Fatalf("%+v", s)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if q.Stats() != c.Stats() {
		t.Fatal("clocks diverge")
	}
	if _, e := c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if q.Stats().Items != 1 || c.Stats().Items != 2 || q.Stats().Now != 2 || c.Stats().Now != 3 {
		t.Fatal("clone aliases original")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i)) + "-x"
			for n := 0; n < 50; n++ {
				now := int64(n)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, n, 0}}})
				_, _ = q.Pop(now, 4)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
				if n%10 == 0 {
					_, _ = q.Clone()
				}
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if s := q.Stats(); s.Items < 0 || s.Items > 256 {
		t.Fatalf("%+v", s)
	}
}
