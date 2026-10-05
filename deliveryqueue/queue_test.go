package deliveryqueue

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
			t.Fatalf("options %+v: got %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := mustQueue(t, 8, 8)
	bad := []string{"", "AB", "a b", "waytoolong", "a.b", "é", "A"}
	for _, id := range bad {
		_, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: id}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: got %v", id, err)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0-9_", "abcd"} {
		if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: id}}}); err != nil {
			t.Fatalf("id %q: got %v", id, err)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	q := mustQueue(t, 8, 8)
	// Unknown kind and negative values must be rejected even alongside valid ops.
	for _, b := range []Batch{
		{Now: -1, Ops: []Op{{Kind: Enqueue, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a"}, {Kind: Kind(99), ID: "b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "bad id"}}},
	} {
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: got %v", b, err)
		}
	}
	if got := q.Snapshot(); got.Generation != 0 || len(got.Items) != 0 {
		t.Fatalf("state mutated: %+v", got)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Kind: Enqueue, ID: "b"}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// Failed batch must not move time backwards or forwards.
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
	// Equal time is allowed.
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "b"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	q := mustQueue(t, 8, 8)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || q.Snapshot().Generation != 0 {
		t.Fatal(r)
	}
	if _, err := q.Apply(Batch{Now: 3, Ops: []Op{{Kind: Enqueue, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if got := q.Snapshot().Generation; got != 1 {
		t.Fatal(got)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "z"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCapacityRollbackRestoresRevision(t *testing.T) {
	q := mustQueue(t, 1, 8)
	r1, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	// Enqueue beyond final capacity fails and rolls back revision allocation.
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "b"}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := q.Snapshot()
	if after.NextRevision != before.NextRevision || after.Generation != before.Generation || len(after.Items) != 1 {
		t.Fatalf("no rollback: %+v -> %+v", before, after)
	}
	// Next successful enqueue reuses the rolled-back revision.
	r2, err := q.Apply(Batch{Ops: []Op{{Kind: Cancel, ID: "a"}, {Kind: Enqueue, ID: "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Revision != r1.Revision+1 {
		t.Fatalf("revisions %d -> %d", r1.Revision, r2.Revision)
	}
}

func TestRevisionMonotonicAcrossBatches(t *testing.T) {
	q := mustQueue(t, 8, 8)
	var last uint64
	for i := 0; i < 5; i++ {
		r, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: fmt.Sprintf("id%d", i)}}})
		if err != nil {
			t.Fatal(err)
		}
		if r.Generation != uint64(i+1) || r.Revision <= last {
			t.Fatal(r)
		}
		last = r.Revision
	}
}

func TestPopOrderingAndAtomicDelete(t *testing.T) {
	q := mustQueue(t, 16, 8)
	_, err := q.Apply(Batch{Ops: []Op{
		{Kind: Enqueue, ID: "b", Priority: 2, ReadyAt: 1},
		{Kind: Enqueue, ID: "a", Priority: 2, ReadyAt: 1},
		{Kind: Enqueue, ID: "c", Priority: 3, ReadyAt: 5},
		{Kind: Enqueue, ID: "d", Priority: 3, ReadyAt: 2},
		{Kind: Enqueue, ID: "e", Priority: 9, ReadyAt: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(10, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"d", "c", "a"} // priority desc, readyAt asc, id asc
	if len(got) != 3 {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// Popped items are gone; not-ready item remains.
	snap := q.Snapshot()
	if len(snap.Items) != 2 || snap.Items[0].ID != "e" || snap.Items[1].ID != "b" {
		t.Fatal(snap.Items)
	}
	// Popping past the end yields whatever is ready.
	got, err = q.Pop(10, 10)
	if err != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got, err)
	}
}

func TestPopInvalidInput(t *testing.T) {
	q := mustQueue(t, 4, 8)
	if _, err := q.Pop(-1, 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	got, err := q.Pop(0, 0)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := mustQueue(t, 4, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a", Priority: 1}}}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "x"})
	again := q.Snapshot()
	if len(again.Items) != 1 || again.Items[0].ID != "a" {
		t.Fatal(again.Items)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q := mustQueue(t, 256, 16)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Kind: Enqueue, ID: id, Priority: i}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Kind: Cancel, ID: id}}})
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	seen := make(map[string]bool)
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatalf("duplicate %q", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestConcurrentEnqueueSameIDSingleWinner(t *testing.T) {
	q := mustQueue(t, 64, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "same"}}}); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, ErrExists) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || len(q.Snapshot().Items) != 1 {
		t.Fatalf("wins=%d items=%d", wins, len(q.Snapshot().Items))
	}
}
