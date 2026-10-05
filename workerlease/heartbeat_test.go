package workerlease

import (
	"errors"
	"fmt"
	"sync"
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
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongkey", "中文"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	good := []string{"a", "z0-_", "12345678"}
	for _, k := range good {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoop(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if s.Generation != 0 || s.Now != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestExpiryBoundaryClosed(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	// ExpiresAt == Now must be evicted before ops run.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// The failed batch rolls back the eviction as well.
	if s := x.Snapshot(); len(s.Entries) != 2 || s.Now != 1 {
		t.Fatal(s)
	}
	// A successful batch at Now=5 evicts "a" (closed boundary).
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != b.Now || s.Generation != b.Generation || s.NextRevision != b.NextRevision {
		t.Fatalf("not rolled back: %+v vs %+v", s, b)
	}
	if len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatal(s)
	}
	r2, _ := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 10}}})
	if r2.Revision != r1.Revision+1 || r2.Generation != r1.Generation+1 {
		t.Fatal(r1, r2)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "zz", 3}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != b.Now || s.Generation != b.Generation || s.NextRevision != b.NextRevision || len(s.Entries) != 1 {
		t.Fatalf("not rolled back: %+v", s)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(9)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if x.Snapshot().Now != 9 {
		t.Fatal(x.Snapshot())
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	s.Entries[0].ExpiresAt = 0
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatal(again)
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
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if seen[e.Revision] {
			t.Fatal("duplicate revision", e.Revision)
		}
		seen[e.Revision] = true
		if e.Revision >= s.NextRevision {
			t.Fatal("revision out of range", e.Revision, s.NextRevision)
		}
	}
}
