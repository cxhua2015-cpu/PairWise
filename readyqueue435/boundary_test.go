package readyqueue435

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 4}, {4, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Kind: Enqueue, ID: ""}}},
		{Ops: []Op{{Kind: Enqueue, ID: "Bad"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "a b"}}},
		{Ops: []Op{{Kind: Enqueue, ID: "toolongidxx"}}},
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
	good := Batch{Now: 1, Ops: []Op{{Kind: Enqueue, ID: "ok_id-1", Priority: -5, ReadyAt: 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Items != 0 || s.Generation != 0 {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 7})
	if e != nil {
		t.Fatal(e)
	}
	if r != (Result{}) {
		t.Fatalf("empty batch result: %+v", r)
	}
	if want := (Snapshot{NextRevision: 1, Items: []Item{}}); !reflect.DeepEqual(want, q.Snapshot()) {
		t.Fatalf("empty batch changed state: %+v", q.Snapshot())
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("failed batch mutated state")
	}
}

func TestDuplicateAndCancelSemantics(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Enqueue then cancel of the same ID within one batch succeeds.
	r, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "b", 0, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if q.Stats().Items != 1 {
		t.Fatal(q.Stats())
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
	// Revision counter must not have advanced.
	r, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
}

func TestPopBoundary(t *testing.T) {
	q := queue(t)
	if _, e := q.Pop(0, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	got, e := q.Pop(0, 0)
	if e != nil || len(got) != 0 {
		t.Fatal(e, got)
	}
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "x", 1, 5}, {Enqueue, "y", 1, 0}}})
	got, _ = q.Pop(4, 10)
	if len(got) != 1 || got[0].ID != "y" {
		t.Fatal(got)
	}
	got, _ = q.Pop(100, 10)
	if len(got) != 1 || got[0].ID != "x" || got[0].Revision != 1 {
		t.Fatal(got)
	}
	if q.Stats().Items != 0 {
		t.Fatal("pop did not delete")
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

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 12})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := string(rune('a'+i%26)) + string(rune('0'+i/26))
			for n := 0; n < 20; n++ {
				_, _ = q.Apply(Batch{Now: int64(n), Ops: []Op{{Enqueue, id, n, 0}}})
				_, _, _, _ = q.Preview(Batch{Now: int64(n), Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(int64(n), 1)
				_ = q.Stats()
				_ = q.Snapshot()
				_, _ = q.Clone()
			}
		}()
	}
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
	if int(s.NextRevision-1) < 0 {
		t.Fatal(s)
	}
}
