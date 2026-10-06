package expirytable239

import (
	"errors"
	"sync"
	"testing"
)

func mustTable(t *testing.T, opts Options) *Table {
	t.Helper()
	x, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {8, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Put, Key: "", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Put, Key: "abcdefghij", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Put, Key: "A", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Put, Key: "a b", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Put, Key: "a", ExpiresAt: 0}}},
		{Ops: []Op{{Kind: Touch, Key: "a", ExpiresAt: 0}}},
		{Ops: []Op{{Kind: Delete, Key: "!", ExpiresAt: 0}}},
	}
	for i, b := range bad {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := x.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := x.ValidateBatch(Batch{Ops: []Op{{Put, "a-b_1", 1}}}); err != nil {
		t.Fatal(err)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	before := x.Snapshot()
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if before.Now != after.Now || before.Generation != after.Generation || before.NextRevision != after.NextRevision {
		t.Fatalf("state leaked: %+v -> %+v", before, after)
	}
}

func TestEvictionRollbackOnCapacity(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	// Now=3 evicts "a" in the candidate, but the final size still exceeds capacity.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 2 || s.Generation != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestEmptyBatchAdvancesTimeWithoutGeneration(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); err != nil {
		t.Fatal(err)
	}
	r, err := x.Apply(Batch{Now: 5})
	if err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Generation != 1 || s.Now != 5 || len(s.Entries) != 0 || r.Generation != 1 {
		t.Fatalf("empty batch: %+v %+v", r, s)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(3)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("expire: %v %+v", err, gone)
	}
	if _, err := x.Expire(2); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}}); err != nil {
		t.Fatal(err)
	}
	if x.Stats().Entries != 1 || c.Stats().Entries != 0 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, Options{MaxEntries: 128, MaxKeyBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for n := 1; n <= 50; n++ {
				now := int64(n)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Kind: Put, Key: key, ExpiresAt: now + 100}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
				_ = x.Stats()
				_ = x.ValidateBatch(Batch{Now: now, Ops: []Op{{Kind: Put, Key: key, ExpiresAt: now + 100}}})
			}
		}()
	}
	wg.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
	if _, err := x.Clone(); err != nil {
		t.Fatal(err)
	}
}
