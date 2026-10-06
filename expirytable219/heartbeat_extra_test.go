package expirytable219

import (
	"errors"
	"fmt"
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

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "中文", "toolongkey"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestExpiryOnApplyRemovesFirst(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}, {Put, "b", 5}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 (closed interval), freeing capacity for "c" and "d".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Revision != 4 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 3 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
	if s.Now != 2 || s.Generation != 2 || s.NextRevision != 5 {
		t.Fatal(s)
	}
}

func TestTouchDeleteNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Touch on an entry expired at the candidate boundary is also not found.
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}}})
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "old", 2}}})
	b := x.Snapshot()
	// Candidate evicts "old" (ExpiresAt=2 <= Now=2) then adds two entries,
	// ending at 3 > MaxEntries: eviction, time and revision must roll back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := x.Snapshot()
	if got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 2 ||
		got.Entries[0].Key != "a" || got.Entries[1].Key != "old" {
		t.Fatal(got)
	}
}

func TestRollbackNotFoundMidBatch(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 1}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != 1 {
		t.Fatal(got)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if x.Snapshot().Now != 5 || len(x.Snapshot().Entries) != 1 {
		t.Fatal(x.Snapshot())
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot shares internal state")
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
			k := fmt.Sprintf("key-%d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("stale entry %+v at now=%d", e, s.Now)
		}
	}
}
