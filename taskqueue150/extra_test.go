package taskqueue150

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidationAndTime(t *testing.T) {
	q := queue(t)
	for _, b := range []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Upper"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "too-long-id"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
	} {
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, err)
		}
	}
	if _, err := q.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(5, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestExistsNotFoundAndRevision(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Cancel, "z", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Failed batches must not consume revisions or bump the generation.
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// Empty batch leaves the generation unchanged.
	if _, err = q.Apply(Batch{Now: 1}); err != nil {
		t.Fatal(err)
	}
	if g := q.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	_, err := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || got.Now != before.Now || len(got.Items) != 1 {
		t.Fatal(got)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 2},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1},
		{Enqueue, "e", 9, 9},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := q.Pop(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "d", "b", "a"} // priority desc, readyAt asc, ID asc; "e" not ready
	if len(items) != len(want) {
		t.Fatal(items)
	}
	for i, id := range want {
		if items[i].ID != id {
			t.Fatal(items)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	p, err := NewPolicy(4, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCoordinator(q, p)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("task-%d", i)
			_, _ = c.Apply("alice", Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = c.Apply("mallory", Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Snapshot()
			_ = c.Decisions()
			if i%3 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	wg.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %+v", i, d)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err = NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}
