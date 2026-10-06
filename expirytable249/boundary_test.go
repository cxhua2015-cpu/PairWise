package expirytable249

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a/b", "toolongkey"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok-1_2", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestClosedIntervalBoundary(t *testing.T) {
	x := table(t)
	// Put with ExpiresAt == Now is structurally invalid (would self-evict).
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 3}, {Put, "b", 2 + 1}}}); e != nil {
		t.Fatal(e)
	}
	// Apply at Now=3 evicts ExpiresAt <= 3 before running ops.
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "e", 5}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Capacity failure must roll back eviction, time and revision.
	if _, e := x.Apply(Batch{Now: 9, Ops: []Op{{Put, "b", 10}, {Put, "c", 11}, {Put, "d", 12}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Eviction of "e" (ExpiresAt 5 <= 9) must be rolled back too.
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 2 ||
		after.Entries[0].Key != "a" || after.Entries[1].Key != "e" {
		t.Fatalf("rollback: %+v vs %+v", before, after)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if _, e := x.Apply(Batch{Now: 1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	cs, xs := c.Stats(), x.Stats()
	if cs != xs {
		t.Fatalf("clocks diverge: %+v %+v", cs, xs)
	}
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); e != nil {
		t.Fatal(e)
	}
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone aliases original")
	}
	// Mutating a returned snapshot must not affect the table.
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 5 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 1000}}})
			_, _ = x.Apply(Batch{Ops: []Op{{Touch, k, 2000}}})
			_ = x.ValidateBatch(Batch{Ops: []Op{{Delete, k, 0}}})
			_ = x.Stats()
			_ = x.Snapshot()
			if c, e := x.Clone(); e == nil {
				_ = c.Stats()
			}
		}()
	}
	w.Wait()
	if n := x.Stats().Entries; n != 32 {
		t.Fatal(n)
	}
	gone, e := x.Expire(2000)
	if e != nil || len(gone) != 32 {
		t.Fatal(e, len(gone))
	}
	if x.Stats().Entries != 0 {
		t.Fatal("expire did not drain")
	}
}
