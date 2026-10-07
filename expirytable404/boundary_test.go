package expirytable404

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -3}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyAndKindValidation(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Put, "", 5}, {Put, "A", 5}, {Put, "a b", 5}, {Put, "a/b", 5},
		{Put, "toolongkey", 5}, {Kind(0), "a", 5}, {Kind(9), "a", 5},
		{Put, "a", 0}, {Touch, "a", 0},
	}
	for _, op := range bad {
		if e := x.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	for _, k := range []string{"a", "z-0_9", "abcdefgh"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 5}}}); e != nil {
			t.Fatalf("%q: %v", k, e)
		}
	}
}

func TestEvictionClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires exactly at Now=2 and must be evicted by the next batch.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Evicts "a" (ExpiresAt 2 <= Now 2) then inserts two keys: final capacity fails.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestRollbackOnNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation || got.NextRevision != b.NextRevision {
		t.Fatal("not rolled back")
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if g := x.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone aliases original")
	}
	if c.Stats() == x.Stats() {
		t.Fatal("clocks should diverge after independent apply")
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
			_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, k, 50}}})
			_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Touch, k, 60}}})
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 70}}})
			_ = x.Stats()
			_ = x.Snapshot()
			_, _ = x.Expire(5)
			if i%4 == 0 {
				_, _ = x.Clone()
			}
		}()
	}
	w.Wait()
	if n := x.Stats().Entries; n != 32 {
		t.Fatal(n)
	}
}
