package expirytable359

import (
	"errors"
	"fmt"
	"reflect"
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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
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
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 100}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "b", 100}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Equal Now is allowed.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 100}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEvictionBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at 2 <= Now, evicted on candidate, freeing capacity for "b".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s.Entries)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Candidate evicts "a" (ExpiresAt 2 <= Now 2) then puts two keys -> capacity failure.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Eviction, time and revision must be rolled back.
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("before=%+v after=%+v", before, x.Snapshot())
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 100}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	// Empty batch: generation unchanged.
	r, _ = x.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	// Delete does not allocate a revision.
	r, _ = x.Apply(Batch{Ops: []Op{{Delete, "a", 0}, {Put, "b", 100}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	if s := x.Snapshot(); s.NextRevision != 3 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestExpireBoundaryAndOwnership(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}, {Put, "c", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	// Mutating the returned slice must not affect internal state.
	gone[0].Key = "zz"
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	s2 := x.Snapshot()
	if len(s2.Entries) != 1 || s2.Entries[0].Key != "c" {
		t.Fatal(s2.Entries)
	}
	if s2.Now != 4 {
		t.Fatal(s2.Now)
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
			k := fmt.Sprintf("k%03d", i)
			for j := 1; j <= 20; j++ {
				now := int64(j)
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 1000}}})
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Touch, k, now + 2000}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
			_, _ = x.Apply(Batch{Now: 30, Ops: []Op{{Delete, k, 0}}})
		}()
	}
	w.Wait()
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}
