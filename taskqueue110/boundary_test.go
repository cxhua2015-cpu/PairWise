package taskqueue110

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -3}, {0, 0}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongid", "a.b", "+x"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefgh", "0", "_", "-"}
	for _, id := range good {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	for _, k := range []Kind{0, 3, 255} {
		_, e := q.Apply(Batch{Ops: []Op{{k, "a", 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestNegativeInput(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	for _, n := range []int{0, -1} {
		if _, e := q.Pop(0, n); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(e)
		}
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
	// Failed batch must not advance time.
	if _, e := q.Apply(Batch{Now: 9, Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal(q.Snapshot().Now)
	}
	// Equal time is allowed.
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Empty batch: no generation bump, revision unchanged.
	r, e = q.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	// Failed batch: revisions not consumed.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}, {Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Cancel does not consume a revision.
	r, _ = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}}})
	if r.Revision != 3 {
		t.Fatal(r)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	// Transiently exceed capacity, then cancel below the limit: allowed.
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "a", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Ending above capacity fails and rolls everything back.
	b := q.Snapshot()
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "x", 1, 0}, {Enqueue, "y", 1, 0}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, q.Snapshot()) {
		t.Fatal(e)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 1}, {Enqueue, "c", 5, 0},
		{Enqueue, "d", 5, 0}, {Enqueue, "e", 9, 10},
	}})
	got, e := q.Pop(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "b"} // priority desc, readyAt asc, id asc
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatal(got)
		}
	}
	// "e" is not ready yet; only "a" remains ready.
	got, _ = q.Pop(1, 10)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatal(got)
	}
	got, _ = q.Pop(10, 10)
	if len(got) != 1 || got[0].ID != "e" {
		t.Fatal(got)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "zz"})
	again := q.Snapshot()
	if len(again.Items) != 1 || again.Items[0].ID != "a" {
		t.Fatal(again)
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "mut"
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
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
				id := fmt.Sprintf("g%d-%04d", g, i)
				_, _ = q.Apply(Batch{Now: int64(i), Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(int64(i), 1)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) > 256 {
		t.Fatal(len(s.Items))
	}
	seen := map[uint64]bool{}
	for _, it := range s.Items {
		if seen[it.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[it.Revision] = true
	}
}

func TestConcurrentEnqueueSameID(t *testing.T) {
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
	ok, dup := 0, 0
	for e := range wins {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrExists) {
			dup++
		}
	}
	if ok != 1 || dup != 15 {
		t.Fatal(ok, dup)
	}
}
