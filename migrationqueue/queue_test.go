package migrationqueue

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {4, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "toolongid", "中文", "a/b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_9", "abcdefgh"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	q := queue(t)
	_, e := q.Apply(Batch{Ops: []Op{{Kind: 99, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = q.Apply(Batch{Ops: []Op{{Kind: 0, ID: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
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
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestRollbackRevisions(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if e != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failing batch must not consume revisions or advance generation.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "a", 1, 0}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r, e)
	}
	s := q.Snapshot()
	if s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 0 || s.Now != 3 {
		t.Fatal(s)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	q, e := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	// Transiently exceeds capacity mid-batch but cancels before the end.
	_, e = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "c", 0, 0},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeding at the end fails and rolls everything back.
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot().Items)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 3, 5},
		{Enqueue, "c", 3, 1},
		{Enqueue, "d", 3, 1},
		{Enqueue, "e", 2, 9},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// now=1: only ready items, priority desc, readyAt asc, id asc.
	got, e := q.Pop(1, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "a"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	// Popped items are gone.
	if len(q.Snapshot().Items) != 2 {
		t.Fatal(q.Snapshot().Items)
	}
	// Limit caps the result.
	got, e = q.Pop(9, 1)
	if e != nil || len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 0, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "mut"
	s.Items = append(s.Items, Item{ID: "x"})
	again := q.Snapshot()
	if len(again.Items) != 2 || again.Items[0].ID != "a" {
		t.Fatal(again.Items)
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "mut"
	if got := q.Snapshot().Items; len(got) != 1 || got[0].ID != "b" {
		t.Fatal("pop result aliases internal state")
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
			id := fmt.Sprintf("task-%d", i)
			for now := int64(0); now < 50; now++ {
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
			t.Fatalf("duplicate id %q", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestConcurrentPopUniqueness(t *testing.T) {
	q, _ := New(Options{MaxItems: 128, MaxIDBytes: 16})
	var ops []Op
	for i := 0; i < 100; i++ {
		ops = append(ops, Op{Enqueue, fmt.Sprintf("t-%d", i), i, 0})
	}
	if _, e := q.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	results := make([][]Item, 10)
	for i := range results {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			var got []Item
			for {
				p, e := q.Pop(0, 3)
				if e != nil || len(p) == 0 {
					break
				}
				got = append(got, p...)
			}
			results[i] = got
		}()
	}
	w.Wait()
	seen := map[string]bool{}
	total := 0
	for _, r := range results {
		for _, it := range r {
			if seen[it.ID] {
				t.Fatalf("id %q popped twice", it.ID)
			}
			seen[it.ID] = true
			total++
		}
	}
	if total != 100 {
		t.Fatal(total)
	}
}
