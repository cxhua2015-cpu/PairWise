package taskqueue170

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
	bad := []string{"", "AB", "a b", "toolong", "a/b", "é", "A", "a.b"}
	for _, id := range bad {
		_, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	for _, id := range []string{"a", "abcd", "a-b_", "0-9z"} {
		if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Second op is structurally invalid; must win over the ErrExists of the first.
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind: 99, ID: "b"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Negative ReadyAt and negative Now are invalid input.
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, -1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := mustQueue(t, 8, 8)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// Failed time moves nothing.
	if s := q.Snapshot(); s.Now != 5 || s.Generation != 1 {
		t.Fatalf("%+v", s)
	}
	// Equal now is allowed.
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionAndGenerationRollback(t *testing.T) {
	q := mustQueue(t, 2, 8)
	r, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	// Failing batch: capacity exceeded at the end.
	_, err = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 2 || len(s.Items) != 1 {
		t.Fatalf("%+v", s)
	}
	// Next successful enqueue reuses the rolled-back revision.
	r, err = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if err != nil || r.Revision != 1+1 || r.Generation != 2 {
		t.Fatal(r, err)
	}
	if got := q.Snapshot().Items[1]; got.ID != "b" || got.Revision != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	q := mustQueue(t, 4, 8)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := mustQueue(t, 8, 8)
	_, err := q.Apply(Batch{Now: 0, Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 9, 100}, // not ready
		{Enqueue, "z", 5, 2},
		{Enqueue, "w", 5, 1},
		{Enqueue, "v", 5, 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(10, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v", "w", "z"} // priority desc, readyAt asc, ID asc
	if len(got) != 3 {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// "y" is not ready at now=10.
	got, err = q.Pop(10, 10)
	if err != nil || len(got) != 1 || got[0].ID != "x" {
		t.Fatal(got, err)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestPopLimit(t *testing.T) {
	q := mustQueue(t, 4, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Pop(0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	got, err := q.Pop(0, 0)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := mustQueue(t, 4, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}}); err != nil {
		t.Fatal(err)
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = s.Items[:0]
	if got := q.Snapshot().Items; len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("%+v", got)
	}
	popped, err := q.Pop(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	popped[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("pop result aliases internal state")
	}
}

func TestExistsNotFound(t *testing.T) {
	q := mustQueue(t, 4, 8)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Enqueue then cancel of the same ID within one batch succeeds.
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "b", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
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
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, int64(i % 3)}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := q.Snapshot()
	seen := make(map[string]bool, len(s.Items))
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatalf("duplicate id %q", it.ID)
		}
		seen[it.ID] = true
		if it.Revision == 0 || it.Revision >= s.NextRevision {
			t.Fatalf("bad revision %+v", it)
		}
	}
}

func TestConcurrentSameIDSingleWinner(t *testing.T) {
	q := mustQueue(t, 64, 8)
	var wg sync.WaitGroup
	wins := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "same", 1, 0}}})
			wins <- err
		}()
	}
	wg.Wait()
	close(wins)
	ok := 0
	for err := range wins {
		if err == nil {
			ok++
		} else if !errors.Is(err, ErrExists) {
			t.Fatal(err)
		}
	}
	if ok != 1 {
		t.Fatalf("winners=%d", ok)
	}
}
