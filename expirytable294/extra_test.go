package expirytable294

import (
	"errors"
	"sync"
	"testing"
)

func TestValidationBoundaries(t *testing.T) {
	if _, e := New(Options{0, 8}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{4, -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	cases := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 1}}},
		{Ops: []Op{{Kind(0), "a", 1}}},
		{Ops: []Op{{Kind(9), "a", 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Ops: []Op{{Touch, "a", 0}}},
	}
	for i, b := range cases {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok-_1", 1}, {Delete, "ok-_1", 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "a", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 || after.Entries[0].ExpiresAt != 5 {
		t.Fatalf("rollback: %+v -> %+v", before, after)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Touch, "ghost", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 0 || s.NextRevision != 1 || len(s.Entries) != 0 {
		t.Fatalf("rollback: %+v", s)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestExpireBoundaryAndClock(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if x.Stats().Now != 3 {
		t.Fatal(x.Stats())
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Generation != 0 {
		t.Fatal("empty batch mutated generation")
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}, {Put, "b", 9}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 1 {
		t.Fatal("alias detected")
	}
	if x.Snapshot().Entries[0].Key != "a" || c.Snapshot().Entries[0].Key != "b" {
		t.Fatal("alias detected")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			for j := 1; j <= 20; j++ {
				now := int64(j)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
				_, _ = x.Expire(now)
				_ = x.Stats()
				_ = x.Snapshot()
				if j%7 == 0 {
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 128 || s.NextRevision < 2 {
		t.Fatalf("stats: %+v", s)
	}
}
