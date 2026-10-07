package readyqueue360

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
	bad := []string{"", "A", "a b", "a.b", "toolongiddd", "中文"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatal(id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatal(id, e)
		}
	}
}

func TestUnknownKindAndEmptyBatch(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	b := q.Snapshot()
	r, e := q.Apply(Batch{Now: 5})
	if e != nil || r != (Result{}) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(r, e, q.Snapshot())
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 10, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(9, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 10 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestPopOrderAndIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0},
	}})
	x, e := q.Pop(0, 10)
	if e != nil || len(x) != 3 || x[0].ID != "c" || x[1].ID != "d" || x[2].ID != "a" {
		t.Fatal(e, x)
	}
	x[0].ID = "zz"
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].ID != "b" {
		t.Fatal(s)
	}
	s.Items[0].ID = "zz"
	if q.Snapshot().Items[0].ID != "b" {
		t.Fatal("snapshot leaks internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%d", i)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
			_, _ = q.Pop(int64(i), 1)
			_ = q.Snapshot()
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}

func TestConcurrentEnqueueSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	var w sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	w.Wait()
	if wins != 1 || len(q.Snapshot().Items) != 1 {
		t.Fatal(wins, q.Snapshot())
	}
}
