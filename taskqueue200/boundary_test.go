package taskqueue200

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before state reads:
	// the unknown kind must win over the duplicate enqueue.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind: 0, ID: "b"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeNow(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
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
	// Failed calls must not move the clock.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRollbackRevisionsAndTime(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	before := q.Snapshot()
	// Fails on the second op: time, items and revisions must roll back.
	_, e := q.Apply(Batch{Now: 9, Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// The next successful enqueue reuses the rolled-back revision.
	r, _ = q.Apply(Batch{Now: 9, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r.Revision != 3 || q.Snapshot().Items[2].Revision != 3 {
		t.Fatal(r, q.Snapshot())
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	before := q.Snapshot()
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Cancel-then-enqueue within one batch fits because capacity is final.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 10})
	if e != nil || r.Generation != 0 || q.Snapshot().Now != 0 {
		t.Fatal(r, e, q.Snapshot())
	}
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	r, _ = q.Apply(Batch{})
	if r.Generation != 1 || q.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestPopSemantics(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 0},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 100}, // not ready
	}})
	got, e := q.Pop(10, 10)
	if e != nil || len(got) != 3 {
		t.Fatal(e, got)
	}
	want := []string{"b", "c", "a"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatal(got)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if _, e = q.Pop(10, 0); !errors.Is(e, ErrInvalidInput) {
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
				id := fmt.Sprintf("g%d-%04d", g, i)
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
	var prev *Item
	for i := range s.Items {
		it := s.Items[i]
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
		if prev != nil && less(it, *prev) {
			t.Fatal("snapshot not in canonical order")
		}
		prev = &s.Items[i]
	}
}

func TestConcurrentSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	var w sync.WaitGroup
	wins := make(chan error, 16)
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "same", 1, 0}}})
			wins <- e
		}()
	}
	w.Wait()
	close(wins)
	ok := 0
	for e := range wins {
		if e == nil {
			ok++
		} else if !errors.Is(e, ErrExists) {
			t.Fatal(e)
		}
	}
	if ok != 1 || len(q.Snapshot().Items) != 1 {
		t.Fatal(ok, len(q.Snapshot().Items))
	}
}
