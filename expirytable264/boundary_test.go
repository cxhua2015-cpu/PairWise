package expirytable264

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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "0", "-", "_", "abcd-12_"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 9}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndExpiryBoundary(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// ExpiresAt <= Now is structurally invalid for Put/Touch.
	if e := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Touch, "a", 4}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.NextRevision != 1 || len(s.Entries) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Final capacity exceeded: evictions, time and revision must roll back.
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Now != b.Now || s.NextRevision != b.NextRevision ||
		s.Generation != b.Generation || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestErrorRollsBackEviction(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// Now=3 would evict "a", but the failing Touch must roll the eviction back.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "zz", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 1 || s.Entries[0].Key != "a" {
		t.Fatalf("eviction not rolled back: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if s := x.Snapshot(); s.Generation != 0 || s.Now != 2 {
		t.Fatalf("%+v", s)
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Delete, "a", 0}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(9)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
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
			_, _ = x.Apply(Batch{Now: 60, Ops: []Op{{Put, k, 100}}})
			_, _ = x.Apply(Batch{Now: 60, Ops: []Op{{Touch, k, 200}}})
			_, _ = x.Expire(50)
			_ = x.Stats()
			_ = x.Snapshot()
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 300}}})
			if c, err := x.Clone(); err == nil {
				_, _ = c.Apply(Batch{Ops: []Op{{Delete, k, 0}}})
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries != 32 || s.Generation != 64 || s.NextRevision != 65 {
		t.Fatalf("%+v", s)
	}
}

func TestCloneIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}})
	c, err := x.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if _, e := c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 60}}}); e != nil {
		t.Fatal(e)
	}
	if len(x.Snapshot().Entries) != 1 || len(c.Snapshot().Entries) != 2 {
		t.Fatal("clone shares state with original")
	}
	if c.Snapshot().Now != 2 || x.Snapshot().Now != 1 {
		t.Fatal("logical clocks not isolated")
	}
}
