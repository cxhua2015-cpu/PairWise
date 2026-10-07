package readyqueue370

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
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatal(id, e)
		}
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "ok_id-1", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 1, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestExistsAndCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	b := q.Snapshot()
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Capacity checked only at the end: cancel-then-enqueue fits, net +1 fails.
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal("state changed after failed batches")
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not consume revisions or bump generation.
	_, _ = q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}})
	r2, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatal(s)
	}
	// Empty batch: generation unchanged.
	r3, _ := q.Apply(Batch{})
	if r3.Generation != 2 || q.Snapshot().Generation != 2 {
		t.Fatal(r3)
	}
	// Revision order matches enqueue order.
	got := map[string]uint64{}
	for _, it := range s.Items {
		got[it.ID] = it.Revision
	}
	if got["a"] != 1 || got["b"] != 2 || got["c"] != 3 {
		t.Fatal(got)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "x", 1, 0},
		{Enqueue, "y", 5, 10}, // not ready at now=1
		{Enqueue, "z", 5, 0},
	}})
	x, e := q.Pop(1, 10)
	if e != nil || len(x) != 2 || x[0].ID != "z" || x[1].ID != "x" {
		t.Fatal(e, x)
	}
	if len(q.Snapshot().Items) != 1 {
		t.Fatal("pop must delete atomically")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	p, _ := q.Pop(0, 1)
	if p[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	p[0].ID = "mutated"
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal("pop result aliases internal state:", e)
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
			for n := 0; n < 50; n++ {
				now := int64(n)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(now, 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
}
