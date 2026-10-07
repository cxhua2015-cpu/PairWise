package readyqueue380

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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongiddd"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c9", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	q := queue(t)
	// Unknown kind must fail even though "zz" is also missing.
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(9), "a", 0, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Invalid ID in a later op must fail before the earlier Cancel reads state.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}, {Enqueue, "BAD", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "t", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2)
	}
	// Empty batch: generation and time unchanged.
	r3, _ := q.Apply(Batch{Now: 1})
	if r3.Generation != 2 {
		t.Fatal(r3)
	}
	if q.Snapshot().Now != 0 {
		t.Fatal("empty batch changed time")
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Failed batch must not consume revisions.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r4, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r4.Revision != 3 {
		t.Fatal(r4)
	}
}

func TestFinalCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	// Net +2 exceeds capacity only at the end; must roll back fully.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// Cancel-then-enqueue within capacity at the end succeeds.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0}, {Cancel, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 0},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 9}, // not ready
		{Enqueue, "e", 2, 0},
	}})
	x, e := q.Pop(1, 2)
	if e != nil || len(x) != 2 || x[0].ID != "b" || x[1].ID != "c" {
		t.Fatal(e, x)
	}
	x, _ = q.Pop(1, 10)
	if len(x) != 2 || x[0].ID != "e" || x[1].ID != "a" {
		t.Fatal(x)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
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
		t.Fatal("pop did not remove")
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
				_, _ = q.Pop(int64(i), 1)
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
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
