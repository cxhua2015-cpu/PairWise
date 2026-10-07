package expirytable409

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, err := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 3})
	bad := []string{"", "A", "a b", "abcd", "a.b", "中文"}
	for _, k := range bad {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "a_1", 9}}}); err != nil {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 9}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := x.ValidateBatch(Batch{Now: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestClosedIntervalExpiry(t *testing.T) {
	x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); err != nil {
		t.Fatal(err)
	}
	// Apply at Now=3 must evict ExpiresAt<=3 inside the candidate state.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if got := x.Snapshot().Entries; len(got) != 1 || got[0].Key != "b" {
		t.Fatalf("%+v", got)
	}
	gone, err := x.Expire(9)
	if err != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(err, gone)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	if err != nil || r.Revision != 1 {
		t.Fatal(err, r)
	}
	before := x.Snapshot()
	// "a" expires at 2 inside the candidate, but the batch still fails on
	// final capacity; expiry, time and revision must all roll back.
	_, err = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("no rollback: %+v -> %+v", before, after)
	}
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Touch, "a", 5}}}); err != nil {
		t.Fatalf("expiry leaked from failed batch: %v", err)
	}
}

func TestNotFoundAndMonotonicTime(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Ops: []Op{{Touch, "a", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Empty batch does not bump generation but still advances time.
	r, err := x.Apply(Batch{Now: 6})
	if err != nil || r.Generation != 0 {
		t.Fatal(err, r)
	}
	if x.Stats().Now != 6 {
		t.Fatal(x.Stats())
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
				if c, err := x.Clone(); err == nil {
					_ = c.Snapshot()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries < 0 || s.Entries > 128 {
		t.Fatal(s)
	}
	if s.NextRevision <= 1 {
		t.Fatal(s)
	}
	seen := map[uint64]bool{}
	for _, e := range x.Snapshot().Entries {
		if seen[e.Revision] || e.Revision >= s.NextRevision {
			t.Fatalf("duplicate or out-of-range revision: %+v", e)
		}
		seen[e.Revision] = true
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 50}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if cs, xs := c.Stats(), x.Stats(); cs != xs {
		t.Fatalf("clocks differ: %+v vs %+v", cs, xs)
	}
	if _, err := c.Apply(Batch{Now: 3, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 0 {
		t.Fatal("clone aliases original")
	}
}
