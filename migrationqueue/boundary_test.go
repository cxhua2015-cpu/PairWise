package migrationqueue

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {0, 0}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a/b", "a.b", "中文", "toolongiddd", "a+b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "z-0_", "abcdefgh", "1", "-", "_"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}, {Cancel, id, 0, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestBatchStructuralValidation(t *testing.T) {
	q := queue(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for _, b := range cases {
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	// Structural validation happens before any state read: an unknown kind
	// must report ErrInvalidInput even alongside an ErrNotFound candidate.
	_, e := q.Apply(Batch{Ops: []Op{{Cancel, "missing", 0, 0}, {Kind: 7, ID: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
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
	if _, e := q.Pop(-1, 1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Failed calls must not move time or state.
	if got := q.Snapshot().Now; got != 5 {
		t.Fatal(got)
	}
	if _, e := q.Apply(Batch{Now: 5, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	r, e = q.Apply(Batch{Now: 4})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 1 {
		t.Fatal(s.Generation)
	}
}

func TestFailureRollsBackRevision(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	before := q.Snapshot()
	// Enqueue allocates a revision, then the batch fails on a missing cancel.
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "zz", 0, 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := q.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	// The next successful enqueue must reuse the rolled-back revision.
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if e != nil || r.Revision != before.NextRevision {
		t.Fatal(r, e, before.NextRevision)
	}
}

func TestCapacityCheckedAtEnd(t *testing.T) {
	q, e := New(Options{MaxItems: 2, MaxIDBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	// Net-zero batch exceeding capacity only transiently must succeed.
	_, e = q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if e != nil {
		t.Fatal(e)
	}
	// Final size above capacity must fail and roll back fully.
	before := q.Snapshot()
	_, e = q.Apply(Batch{Ops: []Op{{Enqueue, "d", 1, 0}, {Enqueue, "e", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, q.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestExistsAndNotFound(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "b", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Cancel then re-enqueue within one batch is allowed.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 3, 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, e := q.Apply(Batch{Now: 10, Ops: []Op{
		{Enqueue, "a", 1, 0},
		{Enqueue, "b", 5, 2},
		{Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1},
		{Enqueue, "e", 9, 100}, // not ready at now=10
	}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := q.Pop(10, 3)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "b"}
	for i, it := range got {
		if it.ID != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if n := len(q.Snapshot().Items); n != 2 {
		t.Fatal(n)
	}
	got, e = q.Pop(100, 10)
	if e != nil || len(got) != 2 || got[0].ID != "e" || got[1].ID != "a" {
		t.Fatal(got, e)
	}
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); e != nil {
		t.Fatal(e)
	}
	s := q.Snapshot()
	s.Items[0].ID = "mutated"
	s.Items[0].Priority = 99
	again := q.Snapshot()
	if again.Items[0].ID != "a" || again.Items[0].Priority != 1 {
		t.Fatal("snapshot shares state with queue")
	}
	popped, e := q.Pop(0, 1)
	if e != nil || len(popped) != 1 {
		t.Fatal(popped, e)
	}
	popped[0].ID = "mutated"
	if n := len(q.Snapshot().Items); n != 0 {
		t.Fatal(n)
	}
}

func TestConcurrentMixed(t *testing.T) {
	q, _ := New(Options{MaxItems: 4096, MaxIDBytes: 16})
	const workers = 16
	var w sync.WaitGroup
	revisions := make([][]uint64, workers)
	for i := 0; i < workers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				id := fmt.Sprintf("w%02d-item%03d", i, j)
				r, e := q.Apply(Batch{Now: int64(j), Ops: []Op{{Enqueue, id, j, 0}}})
				if e == nil {
					revisions[i] = append(revisions[i], r.Revision)
				}
				_, _ = q.Pop(int64(j), 3)
				_ = q.Snapshot()
			}
		}()
	}
	w.Wait()
	seen := map[uint64]bool{}
	total := 0
	for _, rs := range revisions {
		for _, r := range rs {
			if seen[r] {
				t.Fatalf("duplicate revision %d", r)
			}
			seen[r] = true
			total++
		}
	}
	s := q.Snapshot()
	if s.NextRevision != uint64(total)+1 {
		t.Fatalf("next=%d total=%d", s.NextRevision, total)
	}
	if len(s.Items) > 4096 {
		t.Fatal(len(s.Items))
	}
	for _, it := range s.Items {
		if !seen[it.Revision] {
			t.Fatalf("item revision %d not allocated by any apply", it.Revision)
		}
	}
}
