package readyqueue250

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestValidation(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	ok := Batch{Ops: []Op{{Enqueue, "a-z_0", 1, 0}, {Cancel, "a-z_0", 0, 0}}}
	if e := q.ValidateBatch(ok); e != nil {
		t.Fatal(e)
	}
	// Validation is side-effect free.
	if s := q.Snapshot(); len(s.Items) != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
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
	if q.Snapshot().Now != 5 {
		t.Fatal("time rolled back incorrectly")
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Failed batch must not consume revisions or bump generation.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r, e)
	}
	// Empty batch leaves generation unchanged.
	r, e = q.Apply(Batch{})
	if e != nil || r.Generation != 2 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatalf("capacity failure leaked items: %d", n)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 5, 9},
		{Enqueue, "z", 5, 1},
		{Enqueue, "w", 5, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(5, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"w", "z", "x"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 2, 0}}}); e != nil {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 || q.Stats().Now != 3 {
		t.Fatal("clone writes leaked into original")
	}
	if c.Stats().Now != 4 || c.Stats().Items != 2 {
		t.Fatal("clone did not preserve logical clock")
	}
}

func TestConcurrentMix(t *testing.T) {
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
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
}
