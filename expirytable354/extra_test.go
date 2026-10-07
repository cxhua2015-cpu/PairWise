package expirytable354

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "中文", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Kind(9), "b", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeAndMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 10}}}); e != nil {
		t.Fatal(e) // equal Now allowed
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestExpiryEvictionAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "keep", 100}}})
	// "old" expires at Now=2, freeing capacity for the put.
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "new", 50}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "keep" || s.Entries[1].Key != "new" {
		t.Fatal(s.Entries)
	}
	// Failed batch (capacity) rolls back eviction, time and revisions.
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 50, Ops: []Op{{Put, "x", 60}, {Put, "y", 60}, {Put, "z", 60}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision ||
		before.Now != after.Now || len(after.Entries) != len(before.Entries) {
		t.Fatalf("not rolled back: %+v -> %+v", before, after)
	}
	// NotFound also rolls back.
	if _, e = x.Apply(Batch{Now: 50, Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = x.Apply(Batch{Now: 50, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 2 {
		t.Fatal("time leaked from failed batch")
	}
}

func TestExpireClosedBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if s := x.Snapshot(); len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 6 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 4})
	if r.Generation != 1 || x.Snapshot().Generation != 1 {
		t.Fatal(r)
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
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation > 32*20 || s.NextRevision < 2 {
		t.Fatal(s.Generation, s.NextRevision)
	}
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
}
