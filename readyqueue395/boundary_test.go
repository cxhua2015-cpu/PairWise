package readyqueue395

import (
	"errors"
	"fmt"
	"reflect"
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
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid9"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "abcdefgh"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestBatchValidationBeforeState(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Structural error must win over ErrTime.
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Kind: 99, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Negative ReadyAt and negative Now.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Time moved backwards.
	if _, e = q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("time not rolled back")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	// Empty batch: generation unchanged, time still advances.
	r, _ = q.Apply(Batch{Now: 3})
	if r.Generation != 1 || q.Snapshot().Now != 3 {
		t.Fatal(r, q.Snapshot())
	}
	// Failed batch: generation and revision unchanged.
	if _, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
	// Cancel-only success bumps generation, not revision.
	r, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	// Net-zero batch exceeding capacity mid-way is fine.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	// Over capacity at end fails and rolls everything back.
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e, q.Snapshot())
	}
}

func TestPopOrderAndIsolation(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 5}, {Enqueue, "c", 2, 1}, {Enqueue, "d", 2, 1},
	}})
	// Only ready items; ties broken by ReadyAt then ID.
	x, e := q.Pop(1, 10)
	if e != nil || len(x) != 3 || x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatal(e, x)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal(q.Snapshot())
	}
	// Mutating returned slice must not affect the queue.
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	if q.Snapshot().Items[0].ID != "b" {
		t.Fatal("snapshot not isolated")
	}
	if _, e = q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				id := fmt.Sprintf("w%d-%d", i, j)
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, int64(j % 3)}}})
				_, _ = q.Pop(int64(j), 2)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	for i := 1; i < len(s.Items); i++ {
		if less(s.Items[i], s.Items[i-1]) {
			t.Fatal("snapshot not in canonical order")
		}
	}
}
