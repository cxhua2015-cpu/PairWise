package expirytable249

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustTable(t *testing.T, maxEntries, maxKeyBytes int) *Table {
	t.Helper()
	x, err := New(Options{MaxEntries: maxEntries, MaxKeyBytes: maxKeyBytes})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := mustTable(t, 4, 4)
	bad := []string{"", "A", "a b", "a.b", "abcde", "é", "a/b"}
	for _, k := range bad {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	for _, k := range []string{"a", "abcd", "a-1_", "0"} {
		if err := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func TestStructuralErrors(t *testing.T) {
	x := mustTable(t, 4, 8)
	cases := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 1}}},  // negative time
		{Ops: []Op{{Kind(0), "a", 1}}},       // unknown kind
		{Ops: []Op{{Kind(9), "a", 1}}},       // unknown kind
		{Ops: []Op{{Put, "a", 0}}},           // dead on arrival
		{Now: 5, Ops: []Op{{Touch, "a", 5}}}, // expiry at Now
		{Ops: []Op{{Delete, "a", 3}}},        // extra field on delete
	}
	for i, b := range cases {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := x.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if got := x.Snapshot(); got.Generation != 0 || got.Now != 0 || len(got.Entries) != 0 {
		t.Fatalf("failed batches mutated state: %+v", got)
	}
}

func TestClosedIntervalExpiry(t *testing.T) {
	x := mustTable(t, 4, 8)
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	// Apply at Now=5 evicts ExpiresAt <= 5 before running ops.
	if _, err := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}}); err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("entries: %+v", s.Entries)
	}
	// Expire uses the same closed interval.
	gone, err := x.Expire(6)
	if err != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatalf("gone: %v %v", gone, err)
	}
}

func TestRollbackNotFoundAndCapacity(t *testing.T) {
	x := mustTable(t, 2, 8)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}, {Put, "b", 10}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Touch of a missing key fails after a successful put in the same batch.
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 10}, {Touch, "zz", 10}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Final capacity exceeded.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 10}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Delete of a missing key.
	if _, err := x.Apply(Batch{Now: 4, Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Time moved backwards.
	if _, err := x.Apply(Batch{Now: 0, Ops: []Op{{Put, "c", 10}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); got.Generation != before.Generation || got.Now != before.Now ||
		got.NextRevision != before.NextRevision || len(got.Entries) != 2 {
		t.Fatalf("rollback mismatch: %+v vs %+v", got, before)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := mustTable(t, 4, 8)
	r, err := x.Apply(Batch{Now: 0})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, err = x.Apply(Batch{Ops: []Op{{Put, "a", 1}, {Delete, "a", 0}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("delete-only tail: %+v %v", r, err)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
	x := mustTable(t, 4, 8)
	if _, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 5}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Expire(1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 {
		t.Fatal(gone, err)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Now != 5 {
		t.Fatal("expire did not advance clock")
	}
	// Returned snapshot slices are isolated from internal state.
	s := x.Snapshot()
	s.Entries = append(s.Entries, Entry{Key: "fake"})
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	x := mustTable(t, 4, 8)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got != x.Stats() {
		t.Fatalf("clone stats: %+v", got)
	}
	if _, err := c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); err != nil {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 1 || x.Stats().Generation != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if len(c.Snapshot().Entries) != 2 {
		t.Fatal("clone did not evolve independently")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, 128, 16)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key-%d", i)
			for j := 1; j <= 20; j++ {
				now := int64(j)
				if _, err := x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}}); err != nil {
					continue // time may have moved past us; that is fine
				}
			}
			_ = x.Stats()
			_ = x.Snapshot()
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}})
		}()
	}
	wg.Wait()
	s := x.Stats()
	if s.Entries != 32 || s.Generation != s.NextRevision-1 {
		t.Fatalf("stats: %+v", s)
	}
	c, err := x.Clone()
	if err != nil || c.Stats() != s {
		t.Fatalf("clone: %+v %v", c.Stats(), err)
	}
}
