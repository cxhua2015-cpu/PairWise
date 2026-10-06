package expirytable299

import (
	"errors"
	"sync"
	"testing"
)

func TestOptionsAndKeyValidation(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey"} {
		if e := x.ValidateBatch(Batch{Ops: []Op{{Put, k, 5}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok-1_2", 5}}}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralErrors(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", 5}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Put/Touch with ExpiresAt <= Now is already expired: invalid.
	if e := x.ValidateBatch(Batch{Now: 3, Ops: []Op{{Put, "a", 3}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Now: 3, Ops: []Op{{Touch, "a", 2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEvictionBeforeOpsAndNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 and is evicted before Touch runs.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Rollback: eviction of "a" must not be committed.
	if n := len(x.Snapshot().Entries); n != 2 {
		t.Fatal(n)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "zz", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != b.Now || after.NextRevision != b.NextRevision || after.Generation != b.Generation || len(after.Entries) != 1 {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 1})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 1 {
		t.Fatal("empty batch should still advance time")
	}
	r, _ = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Delete, "b", 0}}})
	if r.Generation != 2 {
		t.Fatal(r)
	}
}

func TestExpireClosedBoundAndMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
			k := string(rune('a'+i%26)) + string(rune('a'+i/26))
			_, _ = x.Apply(Batch{Now: int64(i), Ops: []Op{{Put, k, 100}}})
			_, _ = x.Expire(int64(i % 3))
			_ = x.Stats()
			_ = x.Snapshot()
			if c, err := x.Clone(); err == nil {
				_ = c.Snapshot()
			}
			_ = x.ValidateBatch(Batch{Now: int64(i), Ops: []Op{{Put, "z", 100}}})
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != x.Stats().Entries {
		t.Fatal("stats/snapshot mismatch")
	}
}
