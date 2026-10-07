package readyqueue415

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},                                                     // negative batch time
		{Ops: []Op{{Kind: 0, ID: "a"}}},                               // unknown kind
		{Ops: []Op{{Kind: 99, ID: "a"}}},                              // unknown kind
		{Ops: []Op{{Enqueue, "", 1, 0}}},                              // empty id
		{Ops: []Op{{Enqueue, "A", 1, 0}}},                             // uppercase
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},                           // space
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},                    // over MaxIDBytes=8
		{Ops: []Op{{Enqueue, "a", 1, -1}}},                            // negative ReadyAt
		{Ops: []Op{{Cancel, "a", 1, 0}}},                              // cancel with priority
		{Ops: []Op{{Cancel, "a", 0, 1}}},                              // cancel with readyAt
		{Ops: []Op{{Enqueue, "ok", 1, 0}, {Enqueue, "bad id", 1, 0}}}, // trailing bad op
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if s := q.Stats(); s.Generation != 0 || s.Items != 0 || s.Now != 0 {
		t.Fatalf("failed batches mutated state: %+v", s)
	}
	// Valid shapes pass validation.
	if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "a-1_b", 0, 0}, {Cancel, "z", 0, 0}}}); err != nil {
		t.Fatal(err)
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
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(-1, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Pop(5, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := q.Snapshot().Now; got != 5 {
		t.Fatalf("clock moved on failure: %d", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	// Transiently below capacity, but ends above it.
	_, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := q.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Items) != 2 {
		t.Fatalf("capacity failure not rolled back: %+v", after)
	}
	// Duplicate enqueue inside one batch fails with ErrExists and rolls back.
	if _, err = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "c", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if q.Stats().Items != 2 {
		t.Fatal("duplicate batch leaked items")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || q.Stats().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch did not advance clock")
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 10}, // not ready at now=2
		{Enqueue, "z", 5, 1},
		{Enqueue, "w", 5, 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "w" || got[1].ID != "z" || got[2].ID != "x" {
		t.Fatalf("order: %+v", got)
	}
	if q.Stats().Items != 1 {
		t.Fatal("pop did not delete atomically")
	}
	// Revisions are assigned in enqueue order starting at 1.
	if got[2].Revision != 1 || got[0].Revision != 4 {
		t.Fatalf("revisions: %+v", got)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Now != 2 || c.Stats().NextRevision != 2 {
		t.Fatal("clone lost logical clocks")
	}
	if _, err = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Items != 1 || q.Stats().Now != 2 || q.Stats().Generation != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if _, err = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal("original should accept its own b")
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := 0; n < 20; n++ {
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, n, 0}}})
				_ = q.ValidateBatch(Batch{Ops: []Op{{Cancel, id, 0, 0}}})
				_ = q.Stats()
				_ = q.Snapshot()
				_, _ = q.Clone()
				_, _ = q.Pop(int64(n), 1)
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 128 {
		t.Fatalf("stats out of range: %+v", s)
	}
	if got := len(q.Snapshot().Items); got != s.Items {
		t.Fatalf("stats/snapshot disagree: %d vs %d", s.Items, got)
	}
}
