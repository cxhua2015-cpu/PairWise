package resourcelease114

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongkey"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok-key_1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegativeExpiry(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if b.Generation != 0 || b.NextRevision != 1 || len(b.Entries) != 0 {
		t.Fatalf("state mutated: %+v", b)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Now=2 expires "a" in the candidate, but the batch then fails on
	// ErrNotFound, so the eviction must roll back too.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != 1 || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v vs %+v", got, b)
	}
	// Capacity overflow: two live entries with MaxEntries=1.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != 1 || got.NextRevision != 2 || len(got.Entries) != 1 {
		t.Fatalf("capacity rollback failed: %+v", got)
	}
}

func TestClosedBoundaryExpiry(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	// Apply at Now=3 evicts ExpiresAt<=3 from the candidate.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatalf("%+v", s.Entries)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(3); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 5 {
		t.Fatal("now not advanced")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{
					{Put, k, n + 100},
					{Touch, k, n + 200},
				}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 16 {
		t.Fatal(len(s.Entries))
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("stale entry %+v at now=%d", e, s.Now)
		}
	}
}
