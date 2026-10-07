package readyqueue310

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {4, 0}, {-1, 8}, {4, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestInvalidIDs(t *testing.T) {
	q := queue(t)
	for _, id := range []string{"", "A", "a b", "a/b", "toolongiddd", "é"} {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("state changed")
	}
}

func TestUnknownKindAndNegativeReadyAt(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("time rolled back incorrectly")
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	b := q.Snapshot()
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Transient overflow is fine; final overflow must fail and roll back.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "d", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("rollback mismatch")
	}
	// Revision counter must have rolled back too: next enqueue reuses 3.
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}, {Enqueue, "c", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if got := q.Snapshot().Items[1].Revision; got != 3 {
		t.Fatal("revision", got)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 3}); e != nil {
		t.Fatal(e)
	}
	if g := q.Snapshot().Generation; g != 0 {
		t.Fatal("empty batch bumped generation", g)
	}
	if n := q.Snapshot().Now; n != 3 {
		t.Fatal("empty batch must still advance time", n)
	}
	r, e := q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}, {Cancel, "a", 0, 0}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	if g := q.Snapshot().Generation; g != 1 {
		t.Fatal("generation must increase once per batch", g)
	}
}

func TestPopSemantics(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 5},
		{Enqueue, "y", 9, 5},
		{Enqueue, "z", 9, 1},
		{Enqueue, "w", 9, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(1, 10)
	if e != nil || len(got) != 2 || got[0].ID != "w" || got[1].ID != "z" {
		t.Fatal(got, e)
	}
	if got, e = q.Pop(4, 1); e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if _, e = q.Pop(5, -1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	got, e = q.Pop(5, 1)
	if e != nil || len(got) != 1 || got[0].ID != "y" {
		t.Fatal(got, e)
	}
	if n := len(q.Snapshot().Items); n != 1 {
		t.Fatal("remaining", n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items = append(s.Items, Item{ID: "fake"})
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "a" {
		t.Fatal("snapshot aliases internal state", got)
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
				id := fmt.Sprintf("id-%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal("capacity violated")
	}
	if s.NextRevision < 1 {
		t.Fatal("bad revision counter")
	}
}
