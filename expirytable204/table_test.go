package expirytable204

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongkey"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestNegativeAndBackwardsTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != 2 || len(s.Entries) != 1 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestRollbackOnCapacityAndNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Sweep would remove "a" (ExpiresAt 2 <= Now 2), but the batch fails on
	// Touch of a missing key, so the eviction must be rolled back too.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v", got)
	}
	// Capacity failure rolls back revision allocation as well.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.NextRevision != before.NextRevision || got.Now != before.Now {
		t.Fatalf("revision/time leaked: %+v", got)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatalf("empty batch mutated state: %+v", s)
	}
	r, e = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 10}, {Delete, "a", 0}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.NextRevision != 3 || len(s.Entries) != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestDeleteMissingAndExpireBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "nope", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if s := x.Snapshot(); len(s.Entries) != 1 || s.Entries[0].Key != "c" || s.Now != 5 {
		t.Fatalf("%+v", s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	s.Entries[0].ExpiresAt = 1
	if got := x.Snapshot(); got.Entries[0].Key != "a" || got.Entries[0].ExpiresAt != 9 {
		t.Fatalf("snapshot aliases internal state: %+v", got)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	var ok atomic.Uint64
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for n := int64(1); n <= 10; n++ {
				if _, e := x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}}); e == nil {
					ok.Add(1)
				}
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation != ok.Load() {
		t.Fatalf("generation=%d successful applies=%d", s.Generation, ok.Load())
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("expired entry survived: %+v now=%d", e, s.Now)
		}
	}
}
