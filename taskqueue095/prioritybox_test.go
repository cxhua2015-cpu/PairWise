package taskqueue095

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for i, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural error later in the batch must win over an earlier state error.
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}, {Kind: 0, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if g := q.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Cancel-then-enqueue within one batch fits because capacity is checked last.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty batch: no generation bump.
	r, e = q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Cancel-only batch bumps generation but not revision.
	r, e = q.Apply(Batch{Now: 1, Ops: []Op{{Cancel, "a", 0, 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
	it := s.Items[0]
	if it.ID != "b" || it.Revision != 2 {
		t.Fatal(it)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	got, e := q.Pop(0, 10)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, []string{"c", "d", "a"}) {
		t.Fatal(ids)
	}
	// "b" was not ready at now=0.
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("unexpected remaining")
	}
	got, e = q.Pop(1, 1)
	if e != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "fake"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
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
				id := fmt.Sprintf("id-%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n > 256 {
		t.Fatal(n)
	}
}

func TestConcurrentPopUniqueness(t *testing.T) {
	q, _ := New(Options{MaxItems: 200, MaxIDBytes: 16})
	for i := 0; i < 200; i++ {
		_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, fmt.Sprintf("id-%d", i), i, 0}}})
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for {
				items, e := q.Pop(0, 7)
				if e != nil || len(items) == 0 {
					return
				}
				mu.Lock()
				for _, it := range items {
					seen[it.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	w.Wait()
	if len(seen) != 200 {
		t.Fatal(len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("%s popped %d times", id, n)
		}
	}
}
