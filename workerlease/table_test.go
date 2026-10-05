package workerlease

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongkey"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok-key_1", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	if e != nil {
		t.Fatal(e)
	}
	// Batch would expire "a" (ExpiresAt<=Now is false here) and overflow:
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 1 || s.Generation != r.Generation || s.NextRevision != 2 || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("state changed after rollback: %+v", s)
	}
}

func TestRollbackOnMidBatchError(t *testing.T) {
	x, _ := New(Options{MaxEntries: 3, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 50}}})
	b := x.Snapshot()
	// Expires "a" (ExpiresAt 2 <= Now 2), then fails on Touch of missing key.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "zz", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); got.Now != b.Now || got.Generation != b.Generation ||
		got.NextRevision != b.NextRevision || len(got.Entries) != len(b.Entries) {
		t.Fatalf("rollback failed: %+v vs %+v", got, b)
	}
}

func TestExpiryEvictionWithinApply(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	// "a" expires at the closed boundary (ExpiresAt <= Now), freeing capacity.
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatalf("%+v", s)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != b.Generation || r.Revision != b.NextRevision-1 {
		t.Fatal(e, r)
	}
	if got := x.Snapshot(); got.Generation != b.Generation || got.Now != b.Now || got.NextRevision != b.NextRevision {
		t.Fatalf("empty batch mutated state: %+v", got)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "d", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot shares state with table")
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
			k := fmt.Sprintf("w-%d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation > 32*20 || s.Now != 20 {
		t.Fatalf("%+v", s)
	}
	for i := 1; i < len(s.Entries); i++ {
		if s.Entries[i-1].Key >= s.Entries[i].Key {
			t.Fatal("snapshot entries not in canonical key order")
		}
	}
}
