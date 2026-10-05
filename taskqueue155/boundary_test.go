package taskqueue155

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
	bad := []string{"", "A", "a b", "a/b", "toolongid", "中文", "a.b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 10}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 9}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 10 {
		t.Fatal(s.Now)
	}
}

func TestExistsNotFoundAndRevisionRollback(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if s.NextRevision != 2 || s.Generation != 1 || len(s.Items) != 1 {
		t.Fatal(s)
	}
	// Failed multi-op batch must not consume revisions.
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Items[1].Revision != 2 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Temporarily exceed capacity inside the batch, end within it: OK.
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "c", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// End above capacity: whole batch rolls back.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 2 || s.Generation != 1 || s.NextRevision != 4 {
		t.Fatal(s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal(q.Snapshot().Now)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0}, {Enqueue, "d", 5, 0}, {Enqueue, "e", 9, 7},
	}})
	got, e := q.Pop(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "b"}) {
		t.Fatal(ids)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot().Items)
	}
	// "e" is not ready yet.
	got, e = q.Pop(2, 10)
	if e != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got, e)
	}
	got, _ = q.Pop(7, 10)
	if len(got) != 1 || got[0].ID != "e" {
		t.Fatal(got)
	}
	if len(q.Snapshot().Items) != 0 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	if q.Snapshot().Items[0].ID != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "zz"
	if len(q.Snapshot().Items) != 0 {
		t.Fatal("pop result aliases internal state")
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
				id := fmt.Sprintf("g%d-i%02d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 2)
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

func TestConcurrentSameID(t *testing.T) {
	q, _ := New(Options{MaxItems: 4, MaxIDBytes: 8})
	var w sync.WaitGroup
	wins := make(chan error, 16)
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "x", 1, 0}}})
			wins <- e
		}()
	}
	w.Wait()
	close(wins)
	ok := 0
	for e := range wins {
		if e == nil {
			ok++
		} else if !errors.Is(e, ErrExists) {
			t.Fatal(e)
		}
	}
	if ok != 1 || len(q.Snapshot().Items) != 1 {
		t.Fatal(ok, q.Snapshot())
	}
}
