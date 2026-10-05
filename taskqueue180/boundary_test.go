package taskqueue180

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustQueue(t *testing.T, maxItems, maxIDBytes int) *Queue {
	t.Helper()
	q, err := New(Options{MaxItems: maxItems, MaxIDBytes: maxIDBytes})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {0, 0}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := mustQueue(t, 8, 4)
	bad := []string{"", "ABC", "a b", "a.b", "toolong", "a/b", "é"}
	for _, id := range bad {
		_, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	good := []string{"a", "z0-_", "0", "____"}
	for _, id := range good {
		if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestUnknownKindAndNegativeReadyAt(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if s := q.Snapshot(); len(s.Items) != 0 || s.Generation != 0 {
		t.Fatalf("failed batch mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(5, 1); err != nil {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatalf("now=%d", s.Now)
	}
}

func TestExistsNotFoundAndRollback(t *testing.T) {
	q := mustQueue(t, 8, 8)
	r1, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r1.Generation != 1 || r1.Revision != 1 {
		t.Fatal(r1, err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatalf("rollback leaked state: %+v", s)
	}
	r2, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 3, 1}}})
	if err != nil || r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(r2, err)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q := mustQueue(t, 2, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Items) != 2 {
		t.Fatalf("capacity failure not rolled back: %+v", got)
	}
	if _, err = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Cancel, "b", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := mustQueue(t, 8, 8)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, err)
	}
	if s := q.Snapshot(); s.Now != 3 || s.Generation != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := mustQueue(t, 8, 8)
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "low", 1, 0},
		{Enqueue, "hi-b", 5, 2},
		{Enqueue, "hi-a", 5, 2},
		{Enqueue, "early", 5, 1},
		{Enqueue, "future", 9, 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(50, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"early", "hi-a", "hi-b"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if s := q.Snapshot(); len(s.Items) != 2 {
		t.Fatalf("pop not atomic delete: %+v", s)
	}
	got, err = q.Pop(100, 10)
	if err != nil || len(got) != 2 || got[0].ID != "future" || got[1].ID != "low" {
		t.Fatal(got, err)
	}
	if _, err = q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	s.Items[0].ID = "corrupt"
	s.Items[0].Priority = 99
	again := q.Snapshot()
	if again.Items[0].ID != "a" || again.Items[0].Priority != 1 {
		t.Fatalf("snapshot shares state: %+v", again.Items[0])
	}
}

func TestConcurrentMixed(t *testing.T) {
	q := mustQueue(t, 256, 16)
	const workers = 16
	var w sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("task-%02d", i)
			for round := 0; round < 50; round++ {
				now := int64(round + 1)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(now, 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	seen := make(map[string]bool)
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatalf("duplicate id %q", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestConcurrentEnqueueUniqueIDs(t *testing.T) {
	q := mustQueue(t, 128, 16)
	const n = 64
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, fmt.Sprintf("id-%02d", i), i, 0}}}); err != nil {
				t.Error(err)
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) != n || s.Generation != n || s.NextRevision != n+1 {
		t.Fatalf("items=%d generation=%d nextRevision=%d", len(s.Items), s.Generation, s.NextRevision)
	}
	revs := make(map[uint64]bool)
	for _, it := range s.Items {
		if revs[it.Revision] {
			t.Fatalf("duplicate revision %d", it.Revision)
		}
		revs[it.Revision] = true
	}
}
