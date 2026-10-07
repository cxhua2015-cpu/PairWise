package expirytable324

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
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "é"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeAndBackwardsTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestCandidateExpiryClosedBoundary(t *testing.T) {
	x := table(t)
	// Entry with ExpiresAt == Now must be evicted in the candidate state,
	// freeing capacity for the new put.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 5}, {Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 2 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 3 || s.Now != 5 {
		t.Fatal(s)
	}
	for _, en := range s.Entries {
		if en.Key == "a" || en.Key == "b" {
			t.Fatal("stale entry survived", en)
		}
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	r1, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" expires at 2, but the batch still overflows; everything must roll back.
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 2 {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	_ = r1
}

func TestRollbackOnOpError(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch of a missing key fails; the eviction of "a" and revision bump must roll back.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "missing", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != 1 || after.NextRevision != before.NextRevision || len(after.Entries) != 1 {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	r, e := x.Apply(Batch{Now: 5})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != before.Generation {
		t.Fatal(r, before)
	}
	if x.Snapshot().Generation != before.Generation {
		t.Fatal("generation changed on empty batch")
	}
}

func TestExpireBoundaryAndOwnership(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("internal state aliased")
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 10}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 20}}})
				_, _ = x.Expire(n)
				s := x.Snapshot()
				_ = s.Entries
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity exceeded")
	}
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if seen[e.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[e.Revision] = true
	}
}
