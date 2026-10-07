package readyqueue405

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},                                                    // negative time
		{Ops: []Op{{Kind: 0, ID: "a"}}},                              // unknown kind
		{Ops: []Op{{Kind: 99, ID: "a"}}},                             // unknown kind
		{Ops: []Op{{Enqueue, "", 1, 0}}},                             // empty id
		{Ops: []Op{{Enqueue, "ABC", 1, 0}}},                          // uppercase
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},                          // space
		{Ops: []Op{{Enqueue, "a.b", 1, 0}}},                          // punctuation
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}},                    // over MaxIDBytes=8
		{Ops: []Op{{Enqueue, "a", 1, -1}}},                           // negative ReadyAt
		{Ops: []Op{{Cancel, "a", 1, 0}}},                             // cancel extra priority
		{Ops: []Op{{Cancel, "a", 0, 1}}},                             // cancel extra readyAt
		{Ops: []Op{{Enqueue, "ok", 1, 0}, {Cancel, "bad id", 0, 0}}}, // fails as a whole
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	valid := Batch{Now: 5, Ops: []Op{{Enqueue, "ok-id_1", 1, 0}, {Cancel, "gone", 0, 0}}}
	if err := q.ValidateBatch(valid); err != nil {
		t.Fatal(err)
	}
	// Validation is side-effect free: state must be untouched.
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
	// Failed time advance must not change state.
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	// Final capacity is only checked at the end: cancel then two enqueues overflow.
	_, err := q.Apply(Batch{Now: 1, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Generation != before.Generation || got.Now != before.Now ||
		got.NextRevision != before.NextRevision || len(got.Items) != 2 {
		t.Fatalf("no rollback: %+v vs %+v", got, before)
	}
}

func TestExistsNotFoundAndRevision(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Failed batches must not consume revisions.
	r, err = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
	// Empty batch succeeds without bumping generation.
	r, err = q.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 2 {
		t.Fatal(r, err)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 9}, // not ready at now=1
		{Enqueue, "c", 1, 0},
		{Enqueue, "d", 2, 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Pop(1, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	got, err := q.Pop(1, 2)
	if err != nil || len(got) != 2 || got[0].ID != "d" || got[1].ID != "a" {
		t.Fatal(got, err)
	}
	got, err = q.Pop(10, 10)
	if err != nil || len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatal(got, err)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Now != 2 || got.Items != 1 || got.NextRevision != 2 || got.Generation != 1 {
		t.Fatalf("%+v", got)
	}
	// Mutating the clone must not affect the original and vice versa.
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}})
	_, _ = q.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}}})
	if q.Stats().Items != 0 || c.Stats().Items != 2 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("task-%02d", i)
			for n := int64(0); n < 20; n++ {
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(n, 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Now: n, Ops: []Op{{Enqueue, id, i, 0}}})
				if n%5 == 0 {
					_, _ = q.Clone()
				}
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	wg.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 128 {
		t.Fatalf("%+v", s)
	}
}
