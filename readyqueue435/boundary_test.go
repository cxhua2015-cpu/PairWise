package readyqueue435

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestOptionsAndInputBoundaries(t *testing.T) {
	if _, e := New(Options{MaxItems: 0, MaxIDBytes: 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxItems: 1, MaxIDBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "abcdefghij"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Ab"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a", ReadyAt: -1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", Priority: 1}}},
		{Ops: []Op{{Kind: Cancel, ID: "a", ReadyAt: 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	good := Batch{Now: 1, Ops: []Op{{Kind: Enqueue, ID: "a_1-x", Priority: -5, ReadyAt: 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndEmptyBatch(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	r, e := q.Apply(Batch{Now: 5, Ops: []Op{{Kind: Enqueue, ID: "a"}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	before := q.Snapshot()
	r, e = q.Apply(Batch{Now: 9})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExistsCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}, {Kind: Enqueue, ID: "b"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: Enqueue, ID: "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || len(s.Items) != 2 {
		t.Fatalf("rollback leaked revision/items: %+v", s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("task-%02d", i)
			_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Kind: Enqueue, ID: id, Priority: i, ReadyAt: 0}}})
			_, _, _, _ = q.Preview(Batch{Now: int64(i), Ops: []Op{{Kind: Cancel, ID: id}}})
			_, _ = q.Clone()
			_ = q.Stats()
			_ = q.Snapshot()
			_, _ = q.Pop(int64(i), 1)
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 32 {
		t.Fatal(s)
	}
}
