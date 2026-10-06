package readyqueue255

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongid9"}
	for _, id := range bad {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok-id_1", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
}

func TestStructuralErrors(t *testing.T) {
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
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if s := q.Stats(); s.Generation != 0 || s.Now != 0 || s.Items != 0 {
		t.Fatalf("failed batches mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || q.Stats().Now != 3 {
		t.Fatalf("empty batch: %+v %+v", r, q.Stats())
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}}); err != nil {
		t.Fatal(err)
	}
	before := q.Snapshot()
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 1}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Generation != before.Generation || s.NextRevision != before.NextRevision || len(s.Items) != 1 {
		t.Fatalf("capacity failure not rolled back: %+v", s)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "z", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, err := q.Apply(Batch{Ops: []Op{
		{Enqueue, "later", 9, 10},
		{Enqueue, "low", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "a", 5, 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Pop(5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "low" {
		t.Fatalf("order: %+v", got)
	}
	if q.Stats().Items != 1 {
		t.Fatal("pop did not remove atomically")
	}
	if _, err := q.Pop(0, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
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
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}})
	if q.Stats().Items != 1 || q.Stats().Now != 2 {
		t.Fatal("clone mutation leaked into original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 2)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
				if i%10 == 0 {
					_, _ = q.Clone()
				}
			}
		}()
	}
	wg.Wait()
	if got := q.Stats().Items + countPopped(q); got < 0 {
		t.Fatal(got)
	}
}

func countPopped(q *Queue) int {
	n := 0
	for {
		items, _ := q.Pop(1<<62, 256)
		n += len(items)
		if len(items) == 0 {
			return n
		}
	}
}
