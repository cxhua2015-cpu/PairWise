package readyqueue325

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongiddd", "é"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, id := range good {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndStructuralFirst(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation happens before state reads: an unknown kind must
	// win over ErrExists for an already-present ID.
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Kind(9), "b", 1, 0}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 1); e != nil {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestFailedApplyKeepsTime(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	_, e := q.Apply(Batch{Now: 7, Ops: []Op{{Cancel, "missing", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("time advanced on failure")
	}
}

func TestRevisionRollbackAndOrder(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Revision != 2 || r1.Generation != 1 {
		t.Fatal(r1)
	}
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 1 || len(s.Items) != 2 {
		t.Fatal(s)
	}
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r2.Revision != 3 {
		t.Fatal(r2)
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	// Net size stays within capacity even though the mid-batch size exceeds it.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal("capacity failure mutated state")
	}
}

func TestPopOrderAndAtomicity(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 2},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1},
		{Enqueue, "e", 9, 9},
	}})
	got, e := q.Pop(2, 3)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b"}) {
		t.Fatal(ids)
	}
	left := q.Snapshot().Items
	if len(left) != 2 || left[0].ID != "e" || left[1].ID != "a" {
		t.Fatal(left)
	}
	if got[0].Revision == 0 {
		t.Fatal("missing revision")
	}
}

func TestPopNotReadyAndBadArgs(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 10}}})
	got, e := q.Pop(5, 1)
	if e != nil || len(got) != 0 {
		t.Fatal(e, got)
	}
	if _, e = q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("item lost")
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 2})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
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
			id := fmt.Sprintf("id-%d", i)
			for j := 0; j < 20; j++ {
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, 0}}})
				_, _ = q.Pop(int64(j), 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(j), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}
