package expirytable284

import (
	"errors"
	"sync"
	"testing"
)

func TestCapacityRollbackRestoresAll(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch succeeds, then Put of a second entry exceeds capacity: full rollback.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 20}, {Put, "b", 30}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].ExpiresAt != 10 {
		t.Fatalf("rollback: %+v -> %+v", before, after)
	}
}

func TestErrorRollbackKeepsExpiredEntries(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	// Now=3 would expire "a", but Touch on missing key must roll back the expiry.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "b", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("rollback: %+v", s)
	}
}

func TestExpireClosedBoundaryAndMonotonic(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e = x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal("failed expire must not move time")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestValidateMatchesApply(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4})
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "toolong", 1}}},
		{Ops: []Op{{Put, "Bad", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Put, "a", 0}}}, // ExpiresAt <= Now
		{Ops: []Op{{Touch, "a", 0}}},
	}
	for i, b := range bad {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "o_-1", 1}}}); e != nil {
		t.Fatal(e)
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
			for j := 1; j <= 20; j++ {
				now := int64(j)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
				_, _ = x.Expire(now)
				_ = x.Stats()
				_ = x.Snapshot()
				if c, err := x.Clone(); err == nil {
					_ = c.Stats()
				}
				_ = x.ValidateBatch(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries > 128 || s.Now != 20 {
		t.Fatalf("%+v", s)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	c, _ := x.Clone()
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}})
	if x.Stats().Entries != 1 {
		t.Fatal("clone shares state with original")
	}
}
