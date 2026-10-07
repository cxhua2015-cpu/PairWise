package expirytable339

import (
	"errors"
	"fmt"
	"reflect"
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
	bad := []string{"", "A", "a b", "a/b", "toolongkey", "é", "a.b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "abcd-ef_", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "12345678", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "zz", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestEvictionBeforeOps(t *testing.T) {
	x := table(t)
	// Fill to capacity 3 with entries expiring at 2.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}, {Put, "b", 2}, {Put, "c", 2}}}); e != nil {
		t.Fatal(e)
	}
	// At Now=2 all three are evicted (closed bound), making room for new puts.
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "d", 9}, {Put, "e", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 2 || r.Revision != 5 {
		t.Fatalf("got %+v", r)
	}
	s := x.Snapshot()
	if s.Now != 2 || len(s.Entries) != 2 || s.Entries[0].Key != "d" || s.NextRevision != 6 {
		t.Fatalf("got %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}, {Put, "b", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Would end with 4 entries > MaxEntries=3; evictions, time and revisions roll back.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 100}, {Put, "d", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("state changed: %+v", x.Snapshot())
	}
}

func TestErrorRollbackMidBatch(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" is evicted at Now=2, then Touch on it must fail and roll back the eviction.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("state changed: %+v", x.Snapshot())
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("empty batch should still advance time")
	}
	r, e = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 7}, {Put, "b", 8}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(7)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if x.Snapshot().Now != 7 {
		t.Fatal("expire should advance now")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	s.Entries[0].ExpiresAt = 1
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatalf("snapshot aliases internal state: %+v", again)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	for i := 0; i < 32; i++ {
		k := fmt.Sprintf("k%02d", i)
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1000}}}); e != nil {
			t.Fatal(e)
		}
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, 1000 + n}}})
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
	for i, e := range s.Entries {
		if i > 0 && s.Entries[i-1].Key >= e.Key {
			t.Fatal("entries not in canonical key order")
		}
		if e.Revision == 0 || e.Revision >= s.NextRevision {
			t.Fatalf("bad revision %+v next=%d", e, s.NextRevision)
		}
	}
}
