package readyqueue210

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "A"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "toolongid"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "BAD!"}}},
	}
	for i, b := range cases {
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatal(s)
	}
}

func TestValidationBeforeState(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Duplicate ID exists, but the malformed op must be reported first.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind: 42, ID: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
	if _, e := q.Pop(6, 1); e != nil {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 6 {
		t.Fatal(s.Now)
	}
}

func TestExistsNotFoundCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Net growth beyond capacity fails only at the end and rolls back.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "a" || s.Generation != 1 || s.NextRevision != 2 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 0 {
		t.Fatal(r2)
	}
	r3, _ := q.Apply(Batch{})
	if r3.Generation != 2 {
		t.Fatal(r3)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Items[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "p1", 1, 0},
		{Enqueue, "p2a", 2, 5},
		{Enqueue, "p2b", 2, 3},
		{Enqueue, "p2c", 2, 3},
		{Enqueue, "future", 9, 100},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(10, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"p2b", "p2c", "p2a", "p1"}
	var ids []string
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatal(ids)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("future item must remain")
	}
	if _, e = q.Pop(10, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "b"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i % 3)}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	for i := 1; i < len(s.Items); i++ {
		if less(s.Items[i], s.Items[i-1]) {
			t.Fatal("snapshot not in canonical order")
		}
	}
}
