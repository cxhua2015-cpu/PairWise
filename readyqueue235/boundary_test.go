package readyqueue235

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	q := queue(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, ID: "a"}}},
		{Ops: []Op{{Kind: 99, ID: "a"}}},
		{Ops: []Op{{Enqueue, "", 1, 0}}},
		{Ops: []Op{{Enqueue, "Upper", 1, 0}}},
		{Ops: []Op{{Enqueue, "a b", 1, 0}}},
		{Ops: []Op{{Enqueue, "toolongid9", 1, 0}}},
		{Ops: []Op{{Enqueue, "a", 1, -1}}},
		{Ops: []Op{{Cancel, "a", 1, 0}}},
		{Ops: []Op{{Cancel, "a", 0, 1}}},
	}
	for i, b := range bad {
		if e := q.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := q.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	good := Batch{Now: 5, Ops: []Op{{Enqueue, "a-1_b", -3, 0}, {Cancel, "a-1_b", 0, 0}}}
	if e := q.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
	if s := q.Stats(); s.Now != 0 || s.Generation != 0 || s.Items != 0 {
		t.Fatalf("ValidateBatch mutated state: %+v", s)
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
	if _, e := q.Pop(5, 0); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if q.Snapshot().Now != 5 {
		t.Fatal("failed calls moved the clock")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	q := queue(t)
	r, e := q.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(r, e)
	}
	if q.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	r, _ = q.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "a", 1, 0}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = q.Apply(Batch{Now: 4})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestCapacityRollbackRestoresRevision(t *testing.T) {
	q, _ := New(Options{MaxItems: 1, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	b := q.Snapshot()
	_, e := q.Apply(Batch{Ops: []Op{{Enqueue, "b", 1, 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := q.Snapshot(); s.NextRevision != b.NextRevision || s.Now != b.Now || s.Generation != b.Generation || len(s.Items) != 1 {
		t.Fatalf("rollback: %+v vs %+v", s, b)
	}
	r, _ := q.Apply(Batch{Ops: []Op{{Cancel, "a", 0, 0}, {Enqueue, "c", 1, 0}}})
	if r.Revision != 2 {
		t.Fatalf("revision reused after rollback: %+v", r)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	if _, e := q.Apply(Batch{Ops: []Op{{Enqueue, "a", 2, 0}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := q.Apply(Batch{Ops: []Op{{Cancel, "zz", 0, 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestPopOrderAndReadiness(t *testing.T) {
	q, _ := New(Options{MaxItems: 16, MaxIDBytes: 8})
	_, _ = q.Apply(Batch{Ops: []Op{
		{Enqueue, "a", 1, 0}, {Enqueue, "b", 5, 9}, {Enqueue, "c", 5, 1},
		{Enqueue, "d", 5, 1}, {Enqueue, "e", 7, 10},
	}})
	got, e := q.Pop(5, 10)
	if e != nil {
		t.Fatal(e)
	}
	want := []string{"c", "d", "a"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatal(got)
		}
	}
	if len(q.Snapshot().Items) != 2 {
		t.Fatal("pop did not delete atomically")
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Ops: []Op{{Enqueue, "a", 1, 0}}})
	s := q.Snapshot()
	s.Items[0].ID = "zz"
	p, _ := q.Pop(0, 1)
	p[0].ID = "yy"
	if q.Stats().Items != 0 {
		t.Fatal("mutating returned slices leaked into queue")
	}
}

func TestCloneIndependence(t *testing.T) {
	q := queue(t)
	_, _ = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "a", 1, 0}}})
	c, e := q.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != q.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 3, Ops: []Op{{Enqueue, "b", 1, 0}, {Cancel, "a", 0, 0}}})
	if q.Stats().Items != 1 || q.Stats().Now != 2 {
		t.Fatal("clone writes leaked into original")
	}
	if _, e = q.Apply(Batch{Now: 2, Ops: []Op{{Enqueue, "b", 1, 0}}}); e != nil {
		t.Fatal("original lost independent clock/revision:", e)
	}
}

func TestConcurrentMix(t *testing.T) {
	q, _ := New(Options{MaxItems: 256, MaxIDBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			id := fmt.Sprintf("id-%d", i)
			for n := int64(0); n < 20; n++ {
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Enqueue, id, i, 0}}})
				_ = q.ValidateBatch(Batch{Now: n, Ops: []Op{{Cancel, id, 0, 0}}})
				_, _ = q.Pop(n, 1)
				_, _ = q.Apply(Batch{Now: n, Ops: []Op{{Cancel, id, 0, 0}}})
				_ = q.Stats()
				_ = q.Snapshot()
			}
		}()
	}
	w.Add(1)
	go func() {
		defer w.Done()
		for n := 0; n < 50; n++ {
			c, err := q.Clone()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = c.Pop(int64(n), 4)
		}
	}()
	w.Wait()
	s := q.Stats()
	if s.Items < 0 || s.Items > 256 {
		t.Fatal(s)
	}
	if int(s.NextRevision)-1 < s.Items {
		t.Fatalf("revision counter regressed: %+v", s)
	}
}
