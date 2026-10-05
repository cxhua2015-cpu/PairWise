package taskqueue090

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "toolongid", "é", "a.b"}
	for _, id := range bad {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0", "z9-_"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}}); e != nil {
			t.Fatalf("%q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: -1, Ops: []Op{{Enqueue, "a", 0, 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "a", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Now: 4, Ops: []Op{{Enqueue, "b", 0, 0}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := q.Pop(4, 1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Failed batch must not move time backwards check baseline.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestExistsCancelReenqueue(t *testing.T) {
	q := queue(t)
	r1, _ := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r2, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 5, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	if r2.Revision <= r1.Revision || r2.Generation != r1.Generation+1 {
		t.Fatal(r1, r2)
	}
	it := q.Snapshot().Items[0]
	if it.Priority != 5 || it.Revision != r2.Revision {
		t.Fatal(it)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 0, 0}, {Enqueue, "b", 0, 0}, {Enqueue, "c", 0, 0}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatal(s)
	}
}

func TestPopOrderAndPartial(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 3, 5},
		{Enqueue, "c", 3, 1},
		{Enqueue, "d", 3, 1},
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(4, 10)
	if e != nil {
		t.Fatal(e)
	}
	// b not ready (ReadyAt 5 > 4); among ready: priority 3 first, ReadyAt asc, ID asc.
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if !reflect.DeepEqual(ids, []string{"c", "d", "a"}) || len(got) != 3 {
		t.Fatal(ids)
	}
	got, _ = q.Pop(5, 1)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got)
	}
	if got, _ = q.Pop(5, 1); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	p, _ := q.Pop(0, 1)
	p[0].ID = "yy"
	if q.Snapshot().Generation != 1 {
		t.Fatal("generation changed")
	}
}

func TestEmptyBatch(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 {
		t.Fatal(s)
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
				id := fmt.Sprintf("g%d-i%d", g, i)
				now := int64(i)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(now, 1)
				_ = q.Snapshot()
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Cancel, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	seen := map[string]bool{}
	for _, it := range s.Items {
		if seen[it.ID] {
			t.Fatal("duplicate id", it.ID)
		}
		seen[it.ID] = true
		if it.Revision == 0 || it.Revision >= s.NextRevision {
			t.Fatal("bad revision", it)
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
			_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
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
		t.Fatal(ok)
	}
}
