package readyqueue285

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 0, 0}}},
		{Ops: []Op{{Enqueue, "A", 0, 0}}},
		{Ops: []Op{{Enqueue, "a b", 0, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 0, 0}}}, // > MaxIDBytes=8
		{Ops: []Op{{Enqueue, "a", 0, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok_id-1", 5, 3}, {Cancel, "ok_id-1", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Validation must not mutate state.
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestFailedBatchRollsBackTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Fails at the end on capacity; time must roll back to 3.
	if _, err := q.Apply(Batch{Now: 9, Ops: []Op{
		{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0},
		{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatalf("time not rolled back: %v", err)
	}
	s := q.Snapshot()
	if s.Now != 4 || s.NextRevision != 3 || len(s.Items) != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestExistsNotFoundAndRevisionReuse(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "z", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Failed enqueues must not consume revisions.
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 2})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	r, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 2})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
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
	want := []string{"w", "z", "y"} // priority desc, readyAt asc, ID asc
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 2 {
		t.Fatal(n)
	}
	if got, _ := q.Pop(10, 5); len(got) != 1 || got[0].ID != "x" {
		t.Fatal(got)
	}
	if got, _ := q.Pop(10, 5); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestPopInvalidInput(t *testing.T) {
	q := queue(t)
	for _, args := range [][2]int64{{-1, 1}, {0, 0}, {0, -2}} {
		if _, err := q.Pop(args[0], int(args[1])); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(args, err)
		}
	}
}

func TestReturnedSlicesDetached(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 7, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	a, b := q.Stats(), c.Stats()
	if a != b {
		t.Fatalf("clocks diverge: %+v %+v", a, b)
	}
	_, _ = c.Apply(Batch{Now: 7, Ops: []Op{{Cancel, "a", 0, 0}}})
	if len(q.Snapshot().Items) != 2 || len(c.Snapshot().Items) != 1 {
		t.Fatal("clone aliases original")
	}
	// Revisions continue from the cloned clock.
	r, _ := c.Apply(Batch{Now: 7, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r.Revision != 3 {
		t.Fatal(r)
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
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if int(s.Items) != len(q.Snapshot().Items) {
		t.Fatal("stats/snapshot mismatch")
	}
}
