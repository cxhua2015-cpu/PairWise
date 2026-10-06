package readyqueue235

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndInputBoundaries(t *testing.T) {
	if _, err := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "A"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "toolongiddd"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},
	}
	for i, b := range bad {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok_id-1", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestMonotonicTimeAndCapacity(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := q.Stats()
	if s.Items != 1 || s.NextRevision != 2 || s.Generation != 1 || s.Now != 5 {
		t.Fatalf("rollback rollback: %+v", s)
	}
}

func TestDuplicateEnqueueRollsBack(t *testing.T) {
	q := queue(t)
	before := q.Snapshot()
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := q.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Items) != 0 {
		t.Fatalf("not rolled back: %+v", got)
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
			id := fmt.Sprintf("id-%d", i)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, i, 0}}})
			_ = q.Stats()
			_ = q.Snapshot()
			if i%3 == 0 {
				_, _ = q.Pop(int64(i), 1)
			}
			if i%5 == 0 {
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if int(s.Items) != len(q.Snapshot().Items) {
		t.Fatal("stats/snapshot mismatch")
	}
}
