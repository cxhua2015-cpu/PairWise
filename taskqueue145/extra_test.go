package taskqueue145

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestValidationBoundaries(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q := queue(t)
	cases := []Batch{
		{Now: -1, Ops: nil},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Upper"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "waytoolongid"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
	}
	for i, b := range cases {
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatal(s)
	}
}

func TestMonotonicTimeAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	if _, e = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Failed batch must not consume revisions or advance time.
	r, e = q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty batch: generation unchanged.
	r, e = q.Apply(Batch{Now: 6})
	if e != nil || r.Generation != 2 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Now != 6 || s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestPopOrderAndEmpty(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0},
		{Enqueue, "c", 5, 9}, {Enqueue, "d", 5, 1},
	}})
	got, e := q.Pop(0, 10)
	if e != nil || len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatal(got, e)
	}
	got, _ = q.Pop(10, 1)
	if len(got) != 1 || got[0].ID != "d" {
		t.Fatal(got)
	}
	got, _ = q.Pop(10, 0)
	if len(got) != 0 {
		t.Fatal(got)
	}
	if _, e = q.Pop(9, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = q.Pop(10, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zzz"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	p, _ := NewPolicy(2, []string{"a", "b"})
	c, _ := NewCoordinator(q, p)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "a"
			if i%3 == 0 {
				actor = "denied"
			}
			id := fmt.Sprintf("id-%d", i)
			_, _ = c.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Snapshot()
			_ = c.Decisions()
			if i%5 == 0 {
				_ = p.ReplaceActors([]string{"a", "b", "c"})
			}
		}()
	}
	w.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequences", ds)
		}
	}
}

func TestCoordinatorEngineFailureAudited(t *testing.T) {
	q := queue(t)
	p, _ := NewPolicy(4, []string{"a"})
	c, _ := NewCoordinator(q, p)
	if _, e := c.Apply("a", Batch{Ops: []Op{{Cancel, "ghost", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ds := c.Decisions()
	if len(ds) != 1 || ds[0].Committed || ds[0].Error == "" || ds[0].Sequence != 1 {
		t.Fatal(ds)
	}
}
