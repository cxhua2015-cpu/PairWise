package readyqueue375

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "01234567", "z-9_"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	// Unknown kind later in batch must fail before ErrExists is observed.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}, {Kind(99), "b", 1, 1}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Negative ReadyAt rejected.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Negative batch time rejected.
	if _, e = q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Equal time is allowed.
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
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
		t.Fatal(s)
	}
	// Failed batch does not consume revisions.
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}})
	if _, e = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	for _, it := range q.Snapshot().Items {
		if it.ID == "c" && it.Revision != 3 {
			t.Fatal(it)
		}
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Net growth beyond capacity fails and rolls back fully.
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Items) != 1 {
		t.Fatal(got)
	}
	// Cancel-then-enqueue within one batch fits because capacity is final.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestPopSemantics(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 9, 10},
		{Enqueue, "z", 9, 2},
	}})
	// Nothing ready yet.
	got, e := q.Pop(1, 10)
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	// Limit caps results; highest priority with earliest ReadyAt wins.
	got, e = q.Pop(5, 1)
	if e != nil || len(got) != 1 || got[0].ID != "z" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(10, 10)
	if e != nil || len(got) != 2 || got[0].ID != "y" || got[1].ID != "x" {
		t.Fatal(got, e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	if _, e = q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	got, _ := q.Pop(0, 1)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
	got[0].ID = "mut"
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
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
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i % 3)}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate", it.ID)
		}
		seen[it.ID] = true
	}
}
