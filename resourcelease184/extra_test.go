package resourcelease184

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 16, MaxKeyBytes: 8})
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongkey", "a.b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "0", "-", "_", "a-b_c9", "12345678"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 0, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind: 99, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeTime(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}})
	// Structural error wins over time regression.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Kind: 42, Key: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("rollback mismatch: %+v vs %+v", b, got)
	}
}

func TestEvictionRollbackOnCapacity(t *testing.T) {
	x, e := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 100}}})
	b := x.Snapshot()
	// Now=3 evicts "a" in candidate, but final capacity (b,c,d = 3) fails;
	// eviction, time and revision must roll back.
	_, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "c", 100}, {Put, "d", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("rollback mismatch: %+v vs %+v", b, got)
	}
}

func TestEvictionOnApply(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 3}}})
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
	if r.Generation != 2 || r.Revision != 3 || s.NextRevision != 4 {
		t.Fatal(r, s)
	}
}

func TestGenerationEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, _ = x.Apply(Batch{Now: 7, Ops: []Op{{Put, "a", 100}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 8})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestTouchDeleteSemantics(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 10}}})
	r, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 20}, {Delete, "a", 0}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
	// Put overwrites existing key without growing count.
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "k", 1}}})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "k", 50}}})
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestExpireBoundaryAndIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}, {Put, "c", 7}}})
	gone, e := x.Expire(6)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Now != 6 {
		t.Fatal("now not advanced")
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "c" {
		t.Fatal("snapshot shares memory")
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
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 1000}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 2000}}})
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
	if s.NextRevision != s.Generation+1 {
		t.Fatalf("revision/generation mismatch: %+v", s)
	}
}
