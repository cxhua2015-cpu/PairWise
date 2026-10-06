package readyqueue230

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestValidationBoundaries(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
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
	good := Batch{Now: 3, Ops: []Op{{Enqueue, "ok_id-1", 5, 0}, {Cancel, "ok_id-1", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestTimeMonotonicRollback(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	s := q.Stats()
	if s.Now != 5 || s.Items != 1 || s.NextRevision != 2 {
		t.Fatalf("time failure mutated state: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) || r != (Result{}) {
		t.Fatal(e, r)
	}
	s := q.Stats()
	if s.Items != 0 || s.Generation != 0 || s.NextRevision != 1 || s.Now != 0 {
		t.Fatalf("capacity failure not rolled back: %+v", s)
	}
}

func TestExistsRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if s := q.Stats(); s.Now != 0 || s.Generation != 0 {
		t.Fatalf("empty batch mutated state: %+v", s)
	}
}

func TestRevisionSequence(t *testing.T) {
	q := queue(t)
	r, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 1},
		{Enqueue, "c", 5, 0},
		{Enqueue, "d", 9, 100},
	}})
	x, e := q.Pop(50, 10)
	if e != nil || len(x) != 3 {
		t.Fatal(e, x)
	}
	want := []string{"c", "b", "a"}
	for i, it := range x {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", x, want)
		}
	}
	if s := q.Stats(); s.Items != 1 || s.Now != 50 {
		t.Fatalf("%+v", s)
	}
	if n, _ := q.Pop(50, 0); len(n) != 0 {
		t.Fatal(n)
	}
	if _, e := q.Pop(50, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "x"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal("snapshot aliases queue")
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 2, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverges")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Cancel, "a", 0, 0}}})
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "z", 1, 0}}})
	if q.Stats().Items != 3 || c.Stats().Items != 1 {
		t.Fatal("clone aliases original")
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
			id := fmt.Sprintf("id-%02d", i)
			for n := int64(0); n < 20; n++ {
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(n, 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_ = q.ValidateBatch(Batch{Now: n, Ops: []Op{{Cancel, id, 0, 0}}})
				if n%5 == 0 {
					_, _ = q.Clone()
				}
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatalf("%+v", s)
	}
}
