package resourcelease099

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
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
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegativeExpiry(t *testing.T) {
	x := table(t)
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

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch must not consume revision, generation, or time.
	if s := x.Snapshot(); s.Generation != 0 || s.NextRevision != 1 || s.Now != 0 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Third distinct key overflows even though "a" is deleted in the same batch.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 100}, {Put, "d", 100}, {Delete, "a", 0}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("state mutated on capacity failure: %+v -> %+v", before, after)
	}
}

func TestExpiryEvictionWithinApply(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at the closed boundary Now=5, freeing capacity for "b".
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 5 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundaryAndMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 10, Ops: []Op{{Put, "a", 10}, {Put, "b", 11}}})
	gone, e := x.Expire(10)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(9); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 9, Ops: []Op{{Put, "c", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 0 {
		t.Fatal(s)
	}
	_, _ = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}})
	r, _ = x.Apply(Batch{Now: 5})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	gone, _ := x.Expire(5)
	gone[0].Key = "mut"
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 7}}})
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("expire result aliases internal state")
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
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}, {Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 {
		t.Fatal(len(s.Entries))
	}
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if seen[e.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[e.Revision] = true
	}
}
