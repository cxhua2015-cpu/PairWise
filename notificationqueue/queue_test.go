package notificationqueue

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongid", "a/b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestBatchStructuralValidationFirst(t *testing.T) {
	q := queue(t)
	// Unknown kind must fail even though the first op alone would be valid.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(99), "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	// Negative ReadyAt and negative Now.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatalf("%+v", s)
	}
	// Failed batch must not advance time, state, or revision.
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 9, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatalf("before=%+v after=%+v", b, q.Snapshot())
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty batch: generation unchanged.
	r, e = q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatalf("%+v", s)
	}
	if s.Items[0].Revision != 1 || s.Items[1].Revision != 2 {
		t.Fatalf("%+v", s.Items)
	}
	// Failed batch: generation unchanged.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "zzz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().Generation != 1 {
		t.Fatal(q.Snapshot())
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Cancel then enqueue: intermediate peaks at 3 are fine, final 2 ok.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Cancel, "d", 0, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeding final capacity fails and rolls back.
	b := q.Snapshot()
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "e", 1, 0}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "low", 1, 0},
		{Enqueue, "late", 9, 100},
		{Enqueue, "b2", 5, 2},
		{Enqueue, "b1", 5, 1},
		{Enqueue, "a2", 5, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// "late" is not ready at now=10; order: b1(5,1), a2(5,1) tie ReadyAt -> ID asc, b2(5,2), low(1,0).
	got, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"a2", "b1", "b2", "low"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// Popped items are gone; "late" remains.
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "late" {
		t.Fatalf("%+v", s.Items)
	}
	if _, e = q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot shares state")
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "mutated"
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
		if it.Revision == 0 || it.Revision >= s.NextRevision {
			t.Fatalf("bad revision %+v next=%d", it, s.NextRevision)
		}
	}
}

func TestConcurrentPopUniqueness(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 16})
	ops := make([]Op, 0, 100)
	for i := 0; i < 100; i++ {
		ops = append(ops, Op{Enqueue, fmt.Sprintf("id-%d", i), i % 7, 0})
	}
	if _, e := q.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	claimed := map[string]int{}
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for {
				items, e := q.Pop(0, 5)
				if e != nil || len(items) == 0 {
					return
				}
				mu.Lock()
				for _, it := range items {
					claimed[it.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	w.Wait()
	if len(claimed) != 100 {
		t.Fatal(len(claimed))
	}
	for id, n := range claimed {
		if n != 1 {
			t.Fatalf("id %s popped %d times", id, n)
		}
	}
}
