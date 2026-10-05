package resourcelease084

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "ok", 2}, {Put, "bad!", 2}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "ok", 2}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpiryOnApplyClosedInterval(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	// Advancing to Now=3 must evict "a" (ExpiresAt <= Now) but keep "b".
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, e := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Batch would evict "a" (ExpiresAt 2 <= Now 2) but then exceed capacity;
	// eviction, time and revision must all roll back.
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.Generation != before.Generation ||
		got.NextRevision != before.NextRevision || len(got.Entries) != 1 || got.Entries[0].Key != "a" {
		t.Fatalf("state changed: %+v", got)
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != before.Now || got.NextRevision != before.NextRevision {
		t.Fatal("state changed")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Ops: []Op{{Touch, "a", 10}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 2 || s.NextRevision != 4 {
		t.Fatal(s)
	}
	// Empty batch does not bump generation.
	r, e = x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
}

func TestExpireTimeAndIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if s := x.Snapshot(); len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	var revs atomic.Uint64
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for j := 1; j <= 20; j++ {
				if _, e := x.Apply(Batch{Now: int64(j), Ops: []Op{{Put, k, int64(j + 100)}}}); e == nil {
					revs.Add(1)
				}
				if _, e := x.Apply(Batch{Now: int64(j), Ops: []Op{{Touch, k, int64(j + 200)}}}); e == nil {
					revs.Add(1)
				}
				_, _ = x.Expire(int64(j))
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if seen[e.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[e.Revision] = true
	}
	if s.NextRevision != 1+revs.Load() {
		t.Fatal(s.NextRevision)
	}
}
