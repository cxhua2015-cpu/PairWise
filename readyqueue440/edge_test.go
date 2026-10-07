package readyqueue440

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
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
		{Ops: []Op{{Kind: Cancel, ID: ""}}},
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	ok := Batch{Now: 3, Ops: []Op{{Kind: Enqueue, ID: "a-1_b", Priority: -5, ReadyAt: 0}, {Kind: Cancel, ID: "a-1_b"}}}
	if err := q.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
	if got := q.Stats(); got.Items != 0 || got.Now != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestMonotonicTimeAndGeneration(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// Empty batch: valid, generation unchanged.
	r, err := q.Apply(Batch{Now: 5})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.Now != 5 {
		t.Fatalf("%+v", s)
	}
}

func TestDuplicateAndMissingOps(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := q.Stats().Items; got != 0 {
		t.Fatal("failed batch leaked items")
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "ghost", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestRevisionAssignment(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
	items := q.Snapshot().Items
	rev := map[string]uint64{}
	for _, it := range items {
		rev[it.ID] = it.Revision
	}
	if rev["a"] != 1 || rev["b"] != 2 {
		t.Fatalf("%v", rev)
	}
	// Cancel keeps revision counter moving forward.
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if got := q.Stats().NextRevision; got != 4 {
		t.Fatal(got)
	}
}

func TestPopOrderAndIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	got, err := q.Pop(10, 3)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b"}) {
		t.Fatal(ids)
	}
	got[0].ID = "mutated"
	if q.Snapshot().Items[0].ID == "mutated" {
		t.Fatal("pop result aliases state")
	}
	// Not yet ready items stay queued.
	if _, err := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "z", 9, 100}}}); err != nil {
		t.Fatal(err)
	}
	rest, err := q.Pop(10, 10)
	if err != nil || len(rest) != 1 || rest[0].ID != "a" {
		t.Fatal(rest, err)
	}
	if _, err := q.Pop(10, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].Priority = 99
	if q.Snapshot().Items[0].Priority != 1 {
		t.Fatal("snapshot aliases state")
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), q.Snapshot()) {
		t.Fatal("clone diverges at birth")
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Now != 2 || q.Stats().Items != 1 {
		t.Fatal("clone mutated original")
	}
	if _, err := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().Items != 2 || q.Stats().Items != 2 {
		t.Fatal("queues not independent")
	}
}

func TestPreviewFailureParity(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}})
	batches := []Batch{
		{Now: 0},
		{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}},
		{Now: 1, Ops: []Op{{Cancel, "nope", 0, 0}}},
		{Now: 1, Ops: []Op{{Enqueue, "bad id", 0, 0}}},
		{Now: 1, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}},
	}
	before := q.Snapshot()
	for i, b := range batches {
		r, s, st, err := q.Preview(b)
		c, _ := q.Clone()
		_, wantErr := c.Apply(b)
		if !errors.Is(err, wantErr) {
			t.Fatalf("case %d: got %v want %v", i, err, wantErr)
		}
		if err != nil && (r != (Result{}) || !reflect.DeepEqual(s, Snapshot{}) || !reflect.DeepEqual(st, Stats{})) {
			t.Fatalf("case %d: non-zero values on error", i)
		}
	}
	if !reflect.DeepEqual(q.Snapshot(), before) {
		t.Fatal("preview mutated receiver")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := 0; n < 50; n++ {
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Enqueue, id, n, 0}}})
				_, _, _, _ = q.Preview(Batch{Now: int64(n), Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(int64(n), 4)
				_ = q.Snapshot()
				_ = q.Stats()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal("capacity violated")
	}
}
