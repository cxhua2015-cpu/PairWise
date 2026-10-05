package resourcelease129

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

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Kind: 0, Key: "a"},
		{Kind: 99, Key: "a"},
		{Kind: Put, Key: ""},
		{Kind: Put, Key: "Bad"},
		{Kind: Put, Key: "a b"},
		{Kind: Put, Key: "toolongkey"},
		{Kind: Put, Key: "a", ExpiresAt: -1},
	}
	for _, op := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != 5 || len(s.Entries) != 1 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestExpiryEvictionOnApply(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}, {Put, "b", 5}}}); e != nil {
		t.Fatal(e)
	}
	// Now=2 evicts "a" (closed boundary) before capacity check.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 3 || s.Entries[0].Key != "b" {
		t.Fatalf("%+v", s)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Touch of an entry expired at candidate time is also not found.
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 1 || len(s.Entries) != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("not rolled back: %+v vs %+v", before, after)
	}
}

func TestErrorRollbackWithEviction(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	// Would evict "a" and bump time, but Touch fails: all rolled back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "zz", 5}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("not rolled back: %+v vs %+v", before, after)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Ops: []Op{{Touch, "a", 10}}})
	if r.Generation != 2 || r.Revision != 3 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || s.Generation != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "hacked"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	gone, _ := x.Expire(9)
	gone[0].Key = "hacked"
	if x.Snapshot().Now != 9 {
		t.Fatal("expire did not advance time")
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if len(x.Snapshot().Entries) != 1 {
		t.Fatal("b should survive")
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
	if s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatalf("%+v", s)
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("stale entry %+v at now=%d", e, s.Now)
		}
	}
}
