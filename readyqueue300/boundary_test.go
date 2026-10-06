package readyqueue300

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {4, -2}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z9_-_-_"} {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestStructuralErrors(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},  // extra Priority on Cancel
		{Ops: []Op{{Cancel, "a", 0, 1}}},  // extra ReadyAt on Cancel
		{Ops: []Op{{Enqueue, "a", 1, -1}}}, // negative ReadyAt
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if s := q.Stats(); s.Generation != 0 || s.Items != 0 || s.Now != 0 {
		t.Fatalf("failed batches mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if s := q.Stats(); s.Now != 5 || s.Items != 1 {
		t.Fatalf("time rollback: %+v", s)
	}
	// Equal time is allowed.
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	r, err = q.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	q := queue(t) // MaxItems 4
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0},
		{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0},
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Stats()
	if s.Items != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("capacity rollback: %+v", s)
	}
}

func TestDuplicateAndCancelSemantics(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	// Cancel then re-enqueue within one batch is fine.
	r, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 9, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
	it := q.Snapshot().Items[0]
	if it.Priority != 9 || it.Revision != 2 {
		t.Fatal(it)
	}
}

func TestPopOrderingAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 2},
		{Enqueue, "z", 5, 1},
		{Enqueue, "w", 5, 1},
		{Enqueue, "v", 9, 100}, // not ready
	}})
	got, err := q.Pop(10, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"w", "z", "y"} // ReadyAt asc, ID asc within ties
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got %v want prefix %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 2 {
		t.Fatal(n)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Stats(); s.Now != 2 || s.Generation != 1 || s.NextRevision != 3 || s.Items != 2 {
		t.Fatalf("clone clocks: %+v", s)
	}
	// Mutate clone; original must be unaffected, and vice versa.
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}})
	_, _ = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if q.Stats().Items != 3 || c.Stats().Items != 1 {
		t.Fatal("clone aliases original")
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "z", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "zz", 1, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	wg.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("stats: %+v", s)
	}
}
