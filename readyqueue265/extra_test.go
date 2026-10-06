package readyqueue265

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongiddd"}
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
		{Ops: []Op{{Kind(0), "a", 0, 0}}},
		{Ops: []Op{{Kind(99), "a", 0, 0}}},
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
	// ValidateBatch must not read state: cancel of a missing ID is structurally fine.
	if e := q.ValidateBatch(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
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
	// Failed batch must not move the clock backwards-check baseline.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatalf("now moved on failure: %d", s.Now)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	// Final capacity check: cancel frees a slot, but two enqueues overflow at the end.
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Generation != b.Generation || s.NextRevision != b.NextRevision || len(s.Items) != 2 {
		t.Fatalf("not rolled back: %+v", s)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(r, e)
	}
	z := q.Stats()
	if z.Generation != 2 || z.NextRevision != 3 || z.Items != 1 {
		t.Fatalf("%+v", z)
	}
}

func TestPopOrder(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 3, 5},
		{Enqueue, "c", 3, 1},
		{Enqueue, "d", 3, 1},
		{Enqueue, "e", 2, 0},
	}})
	got, e := q.Pop(4, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "e", "a"} // b not ready at now=4
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop not atomic delete")
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 1}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if s := c.Snapshot(); s.Now != 3 || s.Generation != 1 || s.NextRevision != 3 || len(s.Items) != 2 {
		t.Fatalf("%+v", s)
	}
	_, _ = c.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if len(q.Snapshot().Items) != 2 || q.Snapshot().Now != 3 {
		t.Fatal("clone mutated original")
	}
	if _, e := c.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases state")
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
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.ValidateBatch(Batch{Ops: []Op{{Cancel, id, 0, 0}}})
				_ = q.Stats()
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Snapshot().Items) != len(q.Snapshot().Items) {
		t.Fatal("clone diverged")
	}
}
