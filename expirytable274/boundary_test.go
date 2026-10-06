package expirytable274

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestValidateBatchNoSideEffects(t *testing.T) {
	x := table(t)
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 9, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "bad key", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Now: 3, Ops: []Op{{Touch, "a", 3}}},
	}
	for _, b := range bad {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if e := x.ValidateBatch(Batch{Now: 1, Ops: []Op{{Put, "ok_1-x", 2}}}); e != nil {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 || len(s.Entries) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestExpireClosedBoundaryAndMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e = x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestRollbackOnCapacityAndNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 10}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "zz", 10}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.Now != before.Now ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v", after)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if _, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	r, e = x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%5 == 0 {
					_, _ = x.Expire(n)
				}
			}
		}()
	}
	w.Wait()
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if a, b := x.Snapshot(), c.Snapshot(); a.Generation != b.Generation || a.NextRevision != b.NextRevision || len(a.Entries) != len(b.Entries) {
		t.Fatal("clone diverged")
	}
}

func TestCloneOwnership(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, _ := x.Clone()
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("clone write leaked into original")
	}
	if len(c.Snapshot().Entries) != 1 || c.Snapshot().Entries[0].Key != "b" {
		t.Fatal("clone state wrong")
	}
}
