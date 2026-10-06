package readyqueue295

import (
	"errors"
	"fmt"
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

func TestStructuralValidation(t *testing.T) {
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
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	good := Batch{Now: 3, Ops: []Op{{Enqueue, "ok_id-1", 5, 0}, {Cancel, "ok_id-1", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	// ValidateBatch must not mutate state.
	if s := q.Snapshot(); s.Now != 0 || len(s.Items) != 0 || s.Generation != 0 {
		t.Fatalf("validation leaked state: %+v", s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("time rolled back incorrectly")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
	// Net capacity at end is what matters: cancel+enqueue within one batch fits.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestPopOrderAndValidation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "late", 9, 100},
		{Enqueue, "p1a", 1, 0},
		{Enqueue, "p1b", 1, 0},
		{Enqueue, "p2", 2, 5},
	}})
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	x, e := q.Pop(10, 10)
	if e != nil || len(x) != 3 {
		t.Fatal(e, x)
	}
	want := []string{"p2", "p1a", "p1b"}
	for i, it := range x {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", x, want)
		}
		if it.Revision == 0 {
			t.Fatal("revision not assigned")
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop not atomic delete")
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 7, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clocks not preserved")
	}
	_, _ = c.Apply(Batch{Now: 8, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if q.Stats().Items != 1 || q.Stats().Now != 7 || q.Stats().Generation != 1 {
		t.Fatal("clone writes leaked into original")
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
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("bad stats: %+v", s)
	}
}
