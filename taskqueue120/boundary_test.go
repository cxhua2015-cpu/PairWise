package taskqueue120

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongid", "a.b"}
	for _, id := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z9-_"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegativeTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(0), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind(99), "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
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
	if s := q.Snapshot(); s.Now != 5 || len(s.Items) != 1 {
		t.Fatal(s)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not consume revisions or bump generation.
	if _, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r2, _ := q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "c", 1, 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	// Empty batch is a no-op for generation.
	r3, e := q.Apply(Batch{Now: 3})
	if e != nil || r3.Generation != 2 || q.Snapshot().Now != 2 {
		t.Fatal(r3, e)
	}
	s := q.Snapshot()
	if s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestCancelNotFoundAndPopAtomic(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "ghost", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "x", 5, 10}, {Enqueue, "y", 5, 0}}})
	got, e := q.Pop(5, 10)
	if e != nil || len(got) != 1 || got[0].ID != "y" {
		t.Fatal(got, e)
	}
	// Pop atomically removed y; only x remains and it is not ready.
	if got, _ = q.Pop(5, 10); len(got) != 0 {
		t.Fatal(got)
	}
	if got, _ = q.Pop(10, 1); len(got) != 1 || got[0].ID != "x" {
		t.Fatal(got)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 1}, {Enqueue, "a", 9, 9}, {Enqueue, "c", 9, 1}}})
	s1 := q.Snapshot()
	want := []string{"c", "a", "b"}
	var ids []string
	for _, it := range s1.Items {
		ids = append(ids, it.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatal(ids)
	}
	s1.Items[0].ID = "mut"
	if q.Snapshot().Items[0].ID != "c" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 1000, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-%d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 3)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 1000 {
		t.Fatal(len(s.Items))
	}
	for _, it := range s.Items {
		if it.Revision == 0 || it.Revision >= s.NextRevision {
			t.Fatal(it)
		}
	}
}
