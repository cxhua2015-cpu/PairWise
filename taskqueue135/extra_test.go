package taskqueue135

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	q := queue(t)
	before := q.Snapshot()
	// Unknown kind must be rejected without reading state.
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Bad ID charset / empty / too long.
	for _, id := range []string{"", "Abc", "a b", "toolongid123"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(id, e)
		}
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed by invalid batches")
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(-2, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("time moved on failure")
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1, e)
	}
	// Failed batch must not consume revisions or bump generation.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r2, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2, e)
	}
	// Empty batch: generation unchanged.
	if _, e = q.Apply(Batch{Now: 1}); e != nil {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestPopOrderAndDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{
		{Enqueue, "p1", 5, 3}, {Enqueue, "p2", 5, 1}, {Enqueue, "p3", 9, 9}, {Enqueue, "p4", 5, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(2, 10)
	if e != nil || len(got) != 2 || got[0].ID != "p2" || got[1].ID != "p4" {
		t.Fatal(got, e)
	}
	// p3 not ready at now=2; p1 remains.
	got, e = q.Pop(3, 10)
	if e != nil || len(got) != 1 || got[0].ID != "p1" {
		t.Fatal(got, e)
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
	p, e := NewPolicy(4, []string{"w0", "w1", "w2", "w3"})
	if e != nil {
		t.Fatal(e)
	}
	c, e := NewCoordinator(q, p)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := fmt.Sprintf("w%d", w%4)
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("task-%d-%d", w, i)
				_, _ = c.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
				_ = c.Decisions()
				if i%10 == 0 {
					_ = p.ReplaceActors([]string{"w0", "w1", "w2", "w3"})
				}
			}
		}()
	}
	wg.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence gap", i, d.Sequence)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, e := NewPolicy(0, nil); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := NewPolicy(1, []string{"Bad Name"}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	p, _ := NewPolicy(2, []string{"a"})
	if e := p.Authorize("a", 3); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("ghost", 1); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	if e := p.Authorize("a", 2); e != nil {
		t.Fatal(e)
	}
	if e := p.ReplaceActors([]string{"!!"}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed replacement must keep the old list.
	if e := p.Authorize("a", 1); e != nil {
		t.Fatal(e)
	}
}
