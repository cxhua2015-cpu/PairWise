package readyqueue260

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndInputBoundaries(t *testing.T) {
	if _, err := New(Options{MaxItems: 0, MaxIDBytes: 8}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxItems: 1, MaxIDBytes: 0}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 4})
	bad := []Batch{
		{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}},
		{Ops: []Op{{Kind(0), "a", 1, 0}}},
		{Ops: []Op{{Kind(9), "a", 1, 0}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "abcde", 1, 0}}},
		{Ops: []Op{{Enqueue, "A", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if err := q.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := q.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "a-z0", 1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatalf("failed applies mutated state: %+v", s)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 1}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := q.Pop(4, 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	before := q.Snapshot()
	if _, err := q.Apply(Batch{Now: 6, Ops: []Op{{Enqueue, "b", 1, 1}, {Enqueue, "a", 2, 2}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	after := q.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision ||
		before.Now != after.Now || len(after.Items) != 1 {
		t.Fatalf("rollback mismatch: %+v vs %+v", before, after)
	}
}

func TestFinalCapacityCheckedAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, err := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 1}, {Enqueue, "b", 1, 1}}}); err != nil {
		t.Fatal(err)
	}
	// Net size stays within capacity: cancel then enqueue twice in one batch.
	_, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 1}, {Enqueue, "d", 1, 1}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 1}}}); err != nil {
		t.Fatal(err)
	}
	if got := len(q.Snapshot().Items); got != 2 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	r0, err := q.Apply(Batch{Now: 0})
	if err != nil || r0.Generation != 0 {
		t.Fatal(r0, err)
	}
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r2.Generation != 2 || r2.Revision != 0 {
		t.Fatal(r2)
	}
	s := q.Stats()
	if s.Generation != 2 || s.NextRevision != 3 || s.Items != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 5},
		{Enqueue, "b", 3, 9},
		{Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2},
	}})
	got, err := q.Pop(5, 10)
	if err != nil {
		t.Fatal(err)
	}
	// b is not ready (ReadyAt 9); among ready: priority 3 first, then ReadyAt, then ID.
	want := []string{"c", "d", "a"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop did not remove atomically")
	}
}

func TestCloneIsolation(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, err := q.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Now: 4, Ops: []Op{{Cancel, "a", 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if q.Stats().Items != 1 || c.Stats().Items != 0 {
		t.Fatal("clone not isolated")
	}
	if q.Stats().Now != 3 || c.Stats().Now != 4 {
		t.Fatal("clocks not isolated")
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
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("%+v", s)
	}
}

func TestConcurrentCloneConsistent(t *testing.T) {
	q, _ := New(Options{MaxItems: 64, MaxIDBytes: 8})
	stop := make(chan struct{})
	var w sync.WaitGroup
	w.Add(1)
	go func() {
		defer w.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			id := fmt.Sprintf("k%d", i%64)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
		}
	}()
	for i := 0; i < 100; i++ {
		c, err := q.Clone()
		if err != nil {
			t.Fatal(err)
		}
		s := c.Snapshot()
		if int(s.NextRevision) < 1 || len(s.Items) > 64 {
			t.Fatalf("inconsistent clone: %+v", s)
		}
	}
	close(stop)
	w.Wait()
}
