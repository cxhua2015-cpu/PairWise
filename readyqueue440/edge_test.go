package readyqueue440

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestBoundaryValidation(t *testing.T) {
	if _, err := New(Options{MaxItems: 0, MaxIDBytes: 1}); err != ErrInvalidOptions {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxItems: 1, MaxIDBytes: 0}); err != ErrInvalidOptions {
		t.Fatal(err)
	}
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 4})
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "abcde"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "A"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},
	}
	for i, b := range bad {
		if err := q.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); err != ErrInvalidInput {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Kind: Enqueue, ID: "a-b1", Priority: -5}}}); err != nil {
		t.Fatal(err)
	}
}

func TestTimeMonotonicAndEmptyBatch(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	r, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != ErrTime {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); err != ErrTime {
		t.Fatal(err)
	}
	before := q.Stats()
	r, err = q.Apply(Batch{Now: 6})
	if err != nil {
		t.Fatal(err)
	}
	after := q.Stats()
	if after.Generation != before.Generation || after.Now != 6 {
		t.Fatalf("empty batch: before=%+v after=%+v", before, after)
	}
	if r.Generation != before.Generation {
		t.Fatal(r)
	}
}

func TestCapacityRollbackAndDuplicate(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	s := q.Snapshot()
	if len(s.Items) != 2 || s.NextRevision != 3 {
		t.Fatalf("rollback failed: %+v", s)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); err != ErrExists {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 9, 10},
		{Enqueue, "c", 9, 2},
		{Enqueue, "d", 9, 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(3, 10)
	if err != nil || len(got) != 2 || got[0].ID != "c" || got[1].ID != "d" {
		t.Fatal(got, err)
	}
	if _, err := q.Pop(0, 0); err != ErrInvalidInput {
		t.Fatal(err)
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
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
				_ = q.Stats()
				_, _, _, _ = q.Preview(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone lost clocks")
	}
	if _, err := c.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Items != 1 || q.Stats().Now != 3 || q.Stats().Generation != 1 {
		t.Fatalf("clone mutated original: %+v", q.Stats())
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}
