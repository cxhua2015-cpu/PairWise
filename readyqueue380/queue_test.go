package readyqueue380

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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid9", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndBatchPrevalidation(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation of the whole batch happens before any state read:
	// a duplicate ID earlier in the batch must not mask a later invalid op.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Kind(9), "b", 0, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Cancel then re-enqueue within one batch is fine.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 5, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 5, Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 0, 0}, {Enqueue, "d", 0, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Items) != 2 {
		t.Fatal("state not rolled back", got)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 10, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(9, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 10 {
		t.Fatal("time moved on failure")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 3})
	if r.Generation != 1 || q.Snapshot().Generation != 1 {
		t.Fatal("empty batch changed generation", r)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 3, 9},
		{Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2},
		{Enqueue, "e", 2, 1},
	}})
	got, e := q.Pop(5, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "e", "a"} // "b" is not ready at now=5
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatal(got)
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal("pop did not remove", n)
	}
}

func TestPopLimitAndInvalid(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	got, _ := q.Pop(0, 1)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "yy"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state", got)
	}
}

func TestRevisionMonotonicAcrossCancel(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}}})
	_, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 0, 0}}})
	if r1.Revision != 1 || r2.Revision != 2 || q.Snapshot().NextRevision != 3 {
		t.Fatal(r1, r2, q.Snapshot())
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
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal("capacity violated")
	}
	seen := map[uint64]bool{}
	for _, it := range s.Items {
		if seen[it.Revision] {
			t.Fatal("duplicate revision", it)
		}
		seen[it.Revision] = true
	}
}
