package resourcelease159

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}, {0, 0}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := []Op{
		{Kind: 0, Key: "a", ExpiresAt: 1},
		{Kind: 99, Key: "a", ExpiresAt: 1},
		{Kind: Put, Key: "", ExpiresAt: 1},
		{Kind: Put, Key: "A", ExpiresAt: 1},
		{Kind: Put, Key: "a b", ExpiresAt: 1},
		{Kind: Put, Key: "toolongkey", ExpiresAt: 1},
		{Kind: Put, Key: "a", ExpiresAt: -1},
		{Kind: Touch, Key: "a?", ExpiresAt: 1},
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

func TestEvictionClosedInterval(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	// Now=5 evicts "a" (ExpiresAt <= Now) before the new Put, freeing capacity.
	r, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
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
	if _, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" expires at Now=2, but the two Puts still overflow capacity:
	// evictions, time and revision must all roll back.
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("not rolled back: %+v", x.Snapshot())
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "missing", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("not rolled back: %+v", x.Snapshot())
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Delete, "a", 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 1 || s.NextRevision != 3 {
		t.Fatal(s)
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 4}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	s := x.Snapshot()
	if s.Now != 4 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
	// Mutating a snapshot must not affect internal state.
	s2 := x.Snapshot()
	s2.Entries = append(s2.Entries, Entry{Key: "z"})
	if len(x.Snapshot().Entries) != 0 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, e := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
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
}
