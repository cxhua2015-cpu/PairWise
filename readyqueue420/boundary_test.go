package readyqueue420

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文", "a.b"}
	for _, id := range bad {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, id := range good {
		if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); err != nil {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}},
		{Ops: []Op{{Kind(0), "a", 1, 0}}},
		{Ops: []Op{{Kind(99), "a", 1, 0}}},
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
	if s := q.Snapshot(); s.Generation != 0 || len(s.Items) != 0 {
		t.Fatalf("failed validation mutated state: %+v", s)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatalf("time rollback failed: %+v", s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, err := q.Apply(Batch{Now: 10})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if q.Stats().Now != 0 || q.Stats().Generation != 0 {
		t.Fatalf("empty batch mutated state: %+v", q.Stats())
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestRevisionRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if q.Stats().NextRevision != 2 {
		t.Fatalf("revision not rolled back: %+v", q.Stats())
	}
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if r.Revision != 2 {
		t.Fatalf("revision reuse after rollback: %+v", r)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatalf("capacity failure not rolled back: %d items", n)
	}
	_, err = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 3, 9},
		{Enqueue, "z", 3, 2},
		{Enqueue, "w", 3, 2},
	}})
	got, err := q.Pop(5, 10)
	if err != nil || len(got) != 3 {
		t.Fatal(err, got)
	}
	want := []string{"w", "z", "x"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("position %d: got %s want %s", i, it.ID, want[i])
		}
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal(n)
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
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}})
	if q.Stats().Items != 1 || q.Stats().Now != 3 || q.Stats().NextRevision != 2 {
		t.Fatalf("clone writes leaked: %+v", q.Stats())
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-item%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
			}
		}()
	}
	wg.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
}

func TestConcurrentCloneConsistency(t *testing.T) {
	q, _ := New(Options{MaxItems: 64, MaxIDBytes: 16})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("w%d-%d", g, i%8)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}, {Enqueue, id, i, 0}}})
			}
		}(g)
	}
	for i := 0; i < 100; i++ {
		c, err := q.Clone()
		if err != nil {
			t.Fatal(err)
		}
		s := c.Stats()
		snap := c.Snapshot()
		if s.Items != len(snap.Items) || s.Generation != snap.Generation || s.Now != snap.Now || s.NextRevision != snap.NextRevision {
			t.Fatalf("inconsistent clone: %+v vs %+v", s, snap)
		}
	}
	close(stop)
	wg.Wait()
}
