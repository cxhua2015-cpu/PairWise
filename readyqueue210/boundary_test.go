package readyqueue210

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustNew(t *testing.T, maxItems, maxID int) *Queue {
	t.Helper()
	q, err := New(Options{MaxItems: maxItems, MaxIDBytes: maxID})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	q := mustNew(t, 8, 4)
	bad := []Op{
		{Kind: 0, ID: "a"},
		{Kind: 99, ID: "a"},
		{Kind: Enqueue, ID: ""},
		{Kind: Enqueue, ID: "abcde"}, // too long
		{Kind: Enqueue, ID: "A"},     // uppercase
		{Kind: Enqueue, ID: "a b"},   // space
		{Kind: Enqueue, ID: "a.b"},   // dot
		{Kind: Enqueue, ID: "a", Priority: 0, ReadyAt: -1},
	}
	for _, op := range bad {
		if _, err := q.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if _, err := q.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("state mutated by invalid input: %+v", s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := mustNew(t, 8, 8)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(6, 1); err != nil {
		t.Fatal(err)
	}
	if q.Snapshot().Now != 6 {
		t.Fatal("time did not advance")
	}
}

func TestExistsNotFoundAndRevisionRollback(t *testing.T) {
	q := mustNew(t, 8, 8)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 {
		t.Fatalf("revision/generation leaked on failure: %+v", s)
	}
	// Next enqueue must reuse the rolled-back revision.
	r, err = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	q := mustNew(t, 2, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 2 || s.Generation != 1 || s.NextRevision != 3 {
		t.Fatalf("capacity failure not rolled back: %+v", s)
	}
	// Cancel-then-enqueue within one batch fits because capacity is final-only.
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}, {Cancel, "b", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := mustNew(t, 8, 8)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := mustNew(t, 8, 8)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "hacked"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot shares memory with queue")
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q := mustNew(t, 16, 8)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 2},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1},
		{Enqueue, "e", 9, 10}, // not ready
	}})
	got, err := q.Pop(5, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"c", "d", "b"} // priority desc, readyAt asc, id asc
	if len(got) != 3 {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	s := q.Snapshot()
	if len(s.Items) != 2 { // a and e remain
		t.Fatal(s.Items)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q := mustNew(t, 256, 16)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("w%d-%d", w, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal("capacity violated")
	}
	// Revisions must be unique across survivors.
	seen := map[uint64]bool{}
	for _, it := range s.Items {
		if seen[it.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[it.Revision] = true
	}
}
