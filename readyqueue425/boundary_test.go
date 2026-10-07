package readyqueue425

import (
	"errors"
	"reflect"
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
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Upper"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "toolongidxx"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "ok_id-1", Priority: -3}}}); err != nil {
		t.Fatal(err)
	}
	// Validation is side-effect free: state-dependent failures are not reported.
	if err := q.ValidateBatch(Batch{Ops: []Op{{Kind: Cancel, ID: "missing"}}}); err != nil {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 0 || len(s.Items) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestApplyTimeAndEmptyBatch(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if got := q.Stats(); got.Now != 5 || got.Generation != 0 {
		t.Fatalf("empty batch: %+v", got)
	}
	if _, err := q.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestExistsNotFoundAndRevisionRollback(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Failed batches must not consume revisions.
	r, err = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
	if s := q.Stats(); s.NextRevision != 3 || s.Generation != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Over capacity mid-batch is fine as long as the final state fits.
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0},
		{Cancel, "c", 0, 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Final state over capacity fails and rolls back fully.
	before := q.Snapshot()
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0}, {Enqueue, "y", 5, 10}, {Enqueue, "z", 5, 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(3, 10)
	if err != nil || len(got) != 2 || got[0].ID != "z" || got[1].ID != "x" {
		t.Fatal(got, err)
	}
	if _, err := q.Pop(2, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	got, err = q.Pop(10, 0)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	got, err = q.Pop(10, 1)
	if err != nil || len(got) != 1 || got[0].ID != "y" {
		t.Fatal(got, err)
	}
	if _, err := q.Pop(10, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Now != 3 || q.Stats().Items != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if c.Stats().Now != 4 || c.Stats().Items != 0 {
		t.Fatal("clone did not preserve clocks")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i/26)) + string(rune('a'+i%26))
			for n := 0; n < 20; n++ {
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Enqueue, id, n, 0}}})
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(int64(n), 1)
				_ = q.Snapshot()
				_ = q.Stats()
				_, _, _, _ = q.Preview(Batch{Now: int64(n), Ops: []Op{{Enqueue, id, n, 0}}})
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, n, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 128 {
		t.Fatalf("%+v", s)
	}
}
