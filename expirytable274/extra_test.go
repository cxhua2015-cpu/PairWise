package expirytable274

import (
	"errors"
	"sync"
	"testing"
)

func TestValidationBoundaries(t *testing.T) {
	x, err := New(Options{MaxEntries: 2, MaxKeyBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	cases := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 5}}},  // negative time
		{Ops: []Op{{Kind(0), "a", 5}}},       // unknown kind
		{Ops: []Op{{Kind(9), "a", 5}}},       // unknown kind
		{Ops: []Op{{Put, "", 5}}},            // empty key
		{Ops: []Op{{Put, "AB", 5}}},          // uppercase
		{Ops: []Op{{Put, "a b", 5}}},         // space
		{Ops: []Op{{Put, "abcde", 5}}},       // too long
		{Ops: []Op{{Put, "a", 0}}},           // expires at now
		{Ops: []Op{{Delete, "a", 3}}},        // extra field
		{Now: 2, Ops: []Op{{Touch, "a", 2}}}, // touch expiring at now
	}
	for i, b := range cases {
		if err := x.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	good := []Batch{
		{Ops: []Op{{Put, "a-z0", 1}}},
		{Ops: []Op{{Delete, "a", 0}}},
		{Now: 3, Ops: []Op{{Put, "b", 4}}},
	}
	for i, b := range good {
		if err := x.ValidateBatch(b); err != nil {
			t.Fatalf("good case %d: %v", i, err)
		}
	}
	if _, err := New(Options{0, 1}); err != ErrInvalidOptions {
		t.Fatal(err)
	}
	if _, err := New(Options{1, 0}); err != ErrInvalidOptions {
		t.Fatal(err)
	}
}

func TestRollbackOnNotFoundAndCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}, {Put, "b", 10}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Touch missing key mid-batch: full rollback including eviction and revision.
	_, err := x.Apply(Batch{Now: 9, Ops: []Op{{Delete, "a", 0}, {Touch, "ghost", 20}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision ||
		got.Generation != before.Generation || len(got.Entries) != 2 {
		t.Fatalf("state mutated: %+v", got)
	}
	// Capacity failure: rollback.
	_, err = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 10}, {Put, "d", 10}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(x.Snapshot().Entries) != 2 {
		t.Fatal("capacity failure leaked")
	}
	// Eviction happens before ops: expiring entries free capacity.
	if _, err := x.Apply(Batch{Now: 10, Ops: []Op{{Put, "c", 20}, {Put, "d", 20}}}); err != nil {
		t.Fatal(err)
	}
	if n := len(x.Snapshot().Entries); n != 2 {
		t.Fatal(n)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Expire(0); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Expire(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	gone, err := x.Expire(5) // closed interval: ExpiresAt <= now
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(err, gone)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal("now not advanced")
	}
}

func TestCloneIndependence(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 2 {
		t.Fatal("clone shares state")
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
			k := string(rune('a'+i%26)) + string(rune('0'+i/26))
			_, _ = x.Apply(Batch{Now: int64(i), Ops: []Op{{Put, k, 1000}}})
			_, _ = x.Expire(int64(i))
			_ = x.Snapshot()
			_ = x.Stats()
			_, _ = x.Clone()
			_ = x.ValidateBatch(Batch{Now: int64(i), Ops: []Op{{Put, k, 2000}}})
		}()
	}
	w.Wait()
	if got := len(x.Snapshot().Entries); got > 128 {
		t.Fatal(got)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}
