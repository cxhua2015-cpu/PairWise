package settlementqueue

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "é", "toolongid", "a/b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_", "12345678", "a_b-c9"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 1}}})
	// Unknown kind must win over the backwards time.
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Kind: 99, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Bad ID in a later op must fail the whole batch before state reads.
	_, e = q.Apply(Batch{Now: 1, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "BAD", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatal(s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(2, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(3, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed Apply must not move time backwards or forwards.
	if s := q.Snapshot(); s.Now != 3 {
		t.Fatal(s.Now)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 1}, {Enqueue, "b", 1, 1}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	b := q.Snapshot()
	_, e = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "c", 1, 1}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Generation != b.Generation || s.NextRevision != b.NextRevision ||
		s.Now != b.Now || len(s.Items) != 2 {
		t.Fatal(s)
	}
}

func TestRevisionMonotonicAcrossRollback(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
	_, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "zz", 0, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 1}}})
	if r2.Revision != r1.Revision+1 {
		t.Fatal(r1, r2)
	}
	if s := q.Snapshot(); s.NextRevision != r2.Revision+1 {
		t.Fatal(s)
	}
}

func TestEmptyBatch(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 {
		t.Fatal(s)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 2, 9},
		{Enqueue, "c", 2, 1},
		{Enqueue, "d", 2, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	x, e := q.Pop(5, 4)
	if e != nil {
		t.Fatal(e)
	}
	got := []string{x[0].ID, x[1].ID, x[2].ID}
	want := []string{"c", "d", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatal(got)
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("b must remain")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}})
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
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
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

func TestConcurrentDisjointEnqueue(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 32; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("worker-%d", g)
			if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, g, 0}}}); e != nil {
				t.Error(e)
			}
		}()
	}
	w.Wait()
	if n := len(q.Snapshot().Items); n != 32 {
		t.Fatal(n)
	}
}
