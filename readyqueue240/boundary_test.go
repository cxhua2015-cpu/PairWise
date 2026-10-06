package readyqueue240

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "A", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if e := q.ValidateBatch(Batch{Ops: []Op{{Enqueue, "ok_1-x", 5, 3}, {Cancel, "ok_1-x", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 7, Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := q.Snapshot(); got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision {
		t.Fatalf("clock not rolled back: %+v vs %+v", got, b)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if g := q.Stats().Generation; g != 1 {
		t.Fatalf("empty batch bumped generation to %d", g)
	}
}

func TestDuplicateAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.NextRevision != 1 || s.Generation != 0 {
		t.Fatalf("failed batch leaked state: %+v", s)
	}
}

func TestPopSemantics(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{
		{Enqueue, "x", 1, 5}, {Enqueue, "y", 9, 5}, {Enqueue, "z", 9, 1},
	}}); e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(4, 10)
	if e != nil || len(got) != 1 || got[0].ID != "z" || got[0].Revision != 3 {
		t.Fatal(e, got)
	}
	got, e = q.Pop(5, 1)
	if e != nil || len(got) != 1 || got[0].ID != "y" {
		t.Fatal(e, got)
	}
	if got, e = q.Pop(5, 0); e != nil || len(got) != 0 {
		t.Fatal(e, got)
	}
	if _, e = q.Pop(0, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 1 || s.Now != 2 || s.Generation != 1 {
		t.Fatalf("clone write leaked: %+v", s)
	}
	if s := c.Stats(); s.Items != 2 || s.Now != 3 || s.Generation != 2 {
		t.Fatalf("clone lost clocks: %+v", s)
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%d", i)
			for k := 0; k < 50; k++ {
				_, _ = q.Apply(Batch{Now: int64(k), Ops: []Op{{Enqueue, id, k, 0}}})
				_, _ = q.Pop(int64(k), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Now: int64(k), Ops: []Op{{Enqueue, id, k, 0}}})
				if k%10 == 0 {
					_, _ = q.Clone()
				}
				_, _ = q.Apply(Batch{Now: int64(k), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	if s := q.Stats(); s.Items < 0 || s.Items > 256 {
		t.Fatalf("bad stats: %+v", s)
	}
}
