package readyqueue270

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {4, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
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
		{Ops: []Op{{Enqueue, "toolongid", 0, 0}}},
		{Ops: []Op{{Enqueue, "a", 0, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
		{Ops: []Op{{Cancel, "BAD", 0, 0}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	good := []Batch{
		{},
		{Now: 7},
		{Ops: []Op{{Enqueue, "a-1_b", -3, 0}, {Cancel, "a-1_b", 0, 0}}},
	}
	for i, b := range good {
		if e := q.ValidateBatch(b); e != nil {
			t.Fatalf("good case %d: %v", i, e)
		}
	}
	// Validation must be side-effect free.
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 0 || len(s.Items) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestApplyErrors(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 0}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Kind: 5, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	s := q.Stats()
	if s.Generation != 1 || s.NextRevision != 2 || s.Now != 1 || s.Items != 1 {
		t.Fatalf("stats after failures: %+v", s)
	}
}

func TestCapacityCheckedLast(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Net size stays within capacity even though the batch touches 3 IDs.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Net size exceeds capacity: whole batch rolls back.
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty successful batch does not bump the generation.
	r, e = q.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 2},
		{Enqueue, "z", 5, 1},
		{Enqueue, "w", 5, 1},
		{Enqueue, "future", 9, 10},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(5, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"w", "z", "y"} // priority desc, readyAt asc, ID asc
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// "future" is not ready yet; only x remains eligible.
	got, e = q.Pop(5, 10)
	if e != nil || len(got) != 1 || got[0].ID != "x" {
		t.Fatal(got, e)
	}
	got, e = q.Pop(5, 1)
	if e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if _, e = q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 1 {
		t.Fatalf("remaining: %+v", s)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 1}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if got, want := c.Stats(), q.Stats(); got != want {
		t.Fatalf("clone stats %+v want %+v", got, want)
	}
	// Mutating the clone leaves the original untouched and vice versa.
	if _, e = c.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if q.Stats().Items != 2 || q.Stats().Now != 3 {
		t.Fatalf("original changed: %+v", q.Stats())
	}
	if _, e = q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "d", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if c.Stats().Items != 2 || c.Stats().Now != 4 || c.Stats().NextRevision != 4 {
		t.Fatalf("clone changed: %+v vs %+v", c.Stats(), q.Stats())
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%02d", i)
			for j := 0; j < 25; j++ {
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, j, 0}}})
				_ = q.ValidateBatch(Batch{Ops: []Op{{Cancel, id, 0, 0}}})
				_ = q.Stats()
				_ = q.Snapshot()
				if c, err := q.Clone(); err == nil {
					_, _ = c.Pop(0, 1)
				}
				_, _ = q.Pop(0, 1)
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if int(s.Items) != len(q.Snapshot().Items) {
		t.Fatalf("inconsistent state: %+v", s)
	}
}
