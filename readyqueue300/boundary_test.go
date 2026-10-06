package readyqueue300

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

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文", "a.b"}
	for _, id := range bad {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0-9_z"} {
		if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range cases {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if s := q.Stats(); s.Generation != 0 || s.Items != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Now != 5 || s.Items != 1 {
		t.Fatalf("rollback: %+v", s)
	}
}

func TestExistsAndRevision(t *testing.T) {
	q := queue(t)
	r1, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r1.Revision != 1 || r1.Generation != 1 {
		t.Fatal(r1, e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r2, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r2.Revision != 2 {
		t.Fatal(r2, e)
	}
	if g := q.Snapshot().Generation; g != 2 {
		t.Fatal(g)
	}
}

func TestEmptyBatch(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Stats(); s.Generation != 0 {
		t.Fatalf("empty batch bumped generation: %+v", s)
	}
}

func TestPopOrderAndLimit(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
		{Enqueue, "e", 9, 10},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "b"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	rest, e := q.Pop(10, 10)
	if e != nil || len(rest) != 2 || rest[0].ID != "e" || rest[1].ID != "a" {
		t.Fatal(rest, e)
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
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if q.Stats().Items != 1 || q.Stats().Now != 2 || q.Stats().Generation != 1 {
		t.Fatalf("clone write leaked: %+v", q.Stats())
	}
	if c.Stats().Items != 1 || c.Stats().Generation != 2 || c.Stats().NextRevision != 3 {
		t.Fatalf("clone clocks: %+v", c.Stats())
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 512, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("task-%d", i)
			for j := 0; j < 20; j++ {
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, 0}}})
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(int64(j), 1)
				_ = q.Snapshot()
				_ = q.Stats()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	snap := q.Snapshot()
	if s.Items != len(snap.Items) || s.Generation != snap.Generation || s.Now != snap.Now {
		t.Fatalf("inconsistent: %+v vs %+v", s, snap)
	}
}
