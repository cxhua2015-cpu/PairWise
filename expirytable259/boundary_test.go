package expirytable259

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

func TestKeyCharset(t *testing.T) {
	x := table(t)
	ok := []string{"a", "z9-_", "12345678"}
	for _, k := range ok {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "123456789", "a/b"}
	for _, k := range bad {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndExpiryAtNow(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Put/Touch with ExpiresAt <= Now is structurally invalid.
	if e := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Put, "a", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Touch, "a", 4}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Delete ignores ExpiresAt.
	if e := x.ValidateBatch(Batch{Now: 5, Ops: []Op{{Delete, "a", 0}}}); e != nil {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestApplyExpiresBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "keep", 10}}})
	// "old" expires at Now=2 before ops run, freeing capacity for "new".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "new", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "keep" || s.Entries[1].Key != "new" {
		t.Fatal(s.Entries)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 200}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.NextRevision != before.NextRevision ||
		after.Generation != before.Generation || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("not rolled back: %+v -> %+v", before, after)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	// "a" is expired at Now=5, so Touch fails; eviction must roll back too.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if after := x.Snapshot(); after.Now != before.Now || len(after.Entries) != 1 {
		t.Fatalf("not rolled back: %+v", after)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
}

func TestExpireMonotonicAndBoundary(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	if _, e := x.Expire(0); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependence(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}}})
	c, e := x.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if c.Stats() != x.Stats() {
		t.Fatal("clone diverges at birth")
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 60}}})
	if x.Stats().Entries != 1 || c.Stats().Entries != 2 {
		t.Fatal("clone shares state with original")
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
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 1000}}})
			_, _ = x.Apply(Batch{Ops: []Op{{Touch, k, 2000}}})
			_ = x.Stats()
			_ = x.Snapshot()
			_ = x.ValidateBatch(Batch{Ops: []Op{{Put, k, 5}}})
			if i%4 == 0 {
				_, _ = x.Clone()
			}
			if i%8 == 0 {
				_, _ = x.Expire(0)
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries != 32 || s.Generation != 64 || s.NextRevision != 65 {
		t.Fatalf("%+v", s)
	}
}
