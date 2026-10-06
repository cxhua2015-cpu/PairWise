package readyqueue240

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

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z9-_"} {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
	}
	for i, b := range cases {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if s := q.Stats(); s.Generation != 0 || s.Items != 0 {
		t.Fatalf("validation mutated state: %+v", s)
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
	if s := q.Stats(); s.Now != 5 {
		t.Fatalf("now rolled back incorrectly: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	_, err := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 2, 2}, {Cancel, "z", 0, 0}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, err = q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 2, 2}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatalf("rollback mismatch: %+v vs %+v", got, before)
	}
}

func TestExistsAndGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	empty, err := q.Apply(Batch{})
	if err != nil || empty.Generation != 1 {
		t.Fatal(empty, err)
	}
	if s := q.Stats(); s.Generation != 1 || s.NextRevision != 3 {
		t.Fatalf("%+v", s)
	}
}

func TestPopOrderingAndReadiness(t *testing.T) {
	q := queue(t)
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 9, 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "b", "a"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatalf("remaining %d", n)
	}
	if _, err := q.Pop(1, 0); !errors.Is(err, ErrInvalidInput) {
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
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Items != 1 || q.Stats().Now != 2 || q.Stats().Generation != 1 {
		t.Fatalf("original affected: %+v", q.Stats())
	}
	if c.Stats().Items != 1 || c.Stats().Now != 3 || c.Stats().Generation != 2 {
		t.Fatalf("clone state: %+v", c.Stats())
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%02d", i)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Stats()
			_ = q.Snapshot()
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			if i%8 == 0 {
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("stats: %+v", s)
	}
}
