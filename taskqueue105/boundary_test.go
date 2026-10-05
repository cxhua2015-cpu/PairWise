package taskqueue105

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestIDValidation(t *testing.T) {
	q := queue(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongiddd", "a/b"}
	for _, id := range bad {
		_, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, e)
		}
	}
	for _, id := range []string{"a", "a-b_c", "0-9_z", "12345678"} {
		if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, id, 1, 0}}}); e != nil {
			t.Fatalf("id %q: %v", id, e)
		}
	}
}

func TestUnknownKindAndNegativeValues(t *testing.T) {
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
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := q.Pop(0, -1); !errors.Is(e, ErrInvalidInput) {
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
	if _, e := q.Pop(5, 1); e != nil {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.Now != 5 {
		t.Fatal(s.Now)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	q := queue(t)
	b := q.Snapshot()
	r, e := q.Apply(Batch{Now: 10})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r)
	}
	if got := q.Snapshot(); got.Generation != b.Generation || got.Now != b.Now || got.NextRevision != b.NextRevision {
		t.Fatal(got)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}, {Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if s := q.Snapshot(); s.Generation != 1 || s.NextRevision != 3 || len(s.Items) != 1 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	q, _ := New(Options{MaxItems: 2, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	// Net growth fits transiently but final state exceeds capacity.
	_, e := q.Apply(Batch{Now: 1, Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := q.Snapshot()
	if got.Generation != b.Generation || got.NextRevision != b.NextRevision || got.Now != b.Now || len(got.Items) != 1 {
		t.Fatal(got)
	}
	// Intermediate overflow that drains by the end succeeds.
	if _, e = q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}, {Enqueue, "c", 1, 0}, {Cancel, "b", 0, 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExistsNotFoundRollback(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Re-enqueue within one batch after cancel is allowed.
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "a", 5, 9}}}); e != nil {
		t.Fatal(e)
	}
	s := q.Snapshot()
	if len(s.Items) != 1 || s.Items[0].Priority != 5 || s.Items[0].Revision != 2 {
		t.Fatal(s)
	}
}

func TestPopOrderAndAtomicDelete(t *testing.T) {
	q, _ := New(Options{MaxItems: 8, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 3, 5}, {Enqueue, "c", 3, 2},
		{Enqueue, "d", 3, 2}, {Enqueue, "e", 9, 100},
	}})
	got, e := q.Pop(2, 10)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if fmt.Sprint(ids) != "[c d a]" {
		t.Fatal(ids)
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal("pop must delete atomically")
	}
	got, _ = q.Pop(100, 1)
	if len(got) != 1 || got[0].ID != "e" {
		t.Fatal(got)
	}
	if got, _ = q.Pop(100, 0); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	s.Items = append(s.Items, Item{ID: "hack"})
	if got := q.Snapshot(); len(got.Items) != 1 || got.Items[0].ID != "a" {
		t.Fatal(got)
	}
	p, _ := q.Pop(0, 1)
	p[0].ID = "zz"
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
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
				now := int64(i)
				_, _ = q.Apply(Batch{Now: now, Ops: []Op{{Enqueue, id, i, 0}}})
				_, _ = q.Pop(now, 3)
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
		a, b := s.Items[i-1], s.Items[i]
		if a.Priority < b.Priority ||
			(a.Priority == b.Priority && a.ReadyAt > b.ReadyAt) ||
			(a.Priority == b.Priority && a.ReadyAt == b.ReadyAt && a.ID > b.ID) {
			t.Fatal("snapshot not in canonical order")
		}
	}
}

func TestConcurrentRevisionUniqueness(t *testing.T) {
	q, _ := New(Options{MaxItems: 512, MaxIDBytes: 16})
	var w sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		w.Add(1)
		go func() {
			defer w.Done()
			for i := 0; i < 32; i++ {
				id := fmt.Sprintf("w%d-%d", g, i)
				_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, id, 0, 0}}})
			}
		}()
	}
	w.Wait()
	s := q.Snapshot()
	if len(s.Items) != 256 || s.NextRevision != 257 {
		t.Fatal(len(s.Items), s.NextRevision)
	}
	seen := map[uint64]bool{}
	for _, it := range s.Items {
		if seen[it.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[it.Revision] = true
	}
}
