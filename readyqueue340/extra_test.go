package readyqueue340

import (
	"errors"
	"fmt"
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

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "é"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z-9_x"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("time must roll back on failure")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Failed batch must not consume revisions or bump generation.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r.Generation != 3 || r.Revision != 3 {
		t.Fatal(r)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	// Net-zero batch exceeding capacity mid-way must succeed.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Cancel, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Over capacity at end fails and rolls back fully.
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "e", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Generation != b.Generation || s.NextRevision != b.NextRevision || len(s.Items) != len(b.Items) {
		t.Fatal("capacity failure must roll back")
	}
}

func TestExistsAndPopOrder(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 5, 9}, {Enqueue, "c", 5, 1}, {Enqueue, "d", 5, 1}}})
	x, e := q.Pop(10, 10)
	if e != nil || len(x) != 4 {
		t.Fatal(e, x)
	}
	want := []string{"c", "d", "b", "a"} // priority desc, readyAt asc, id asc
	for i := range want {
		if x[i].ID != want[i] {
			t.Fatalf("got %v want %v", x, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	// Not-ready items stay queued.
	_, _ = q.Apply(Batch{Now: 10, Ops: []Op{{Enqueue, "z", 9, 100}}})
	if x, _ = q.Pop(10, 10); len(x) != 0 {
		t.Fatal(x)
	}
	if x, _ = q.Pop(100, 1); len(x) != 1 || x[0].ID != "z" {
		t.Fatal(x)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0] = Item{ID: "hacked"}
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot must be isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 1000, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
	}
}
