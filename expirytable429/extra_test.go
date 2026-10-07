package expirytable429

import (
	"errors"
	"sync"
	"testing"
)

func TestValidationBoundaries(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4})
	bad := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "abcde", 1}}},
		{Ops: []Op{{Put, "A", 1}}},
		{Ops: []Op{{Put, "a b", 1}}},
		{Ops: []Op{{Kind(0), "a", 1}}},
		{Ops: []Op{{Kind(9), "a", 1}}},
		{Ops: []Op{{Put, "a", 0}}},
		{Now: 3, Ops: []Op{{Touch, "a", 3}}},
	}
	for i, b := range bad {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "a-z0", 1}, {Delete, "a", 0}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRollbackNotFoundAndCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// "a" is swept (ExpiresAt 10 <= Now 10), then two puts overflow capacity 2? no: fits.
	// Use a batch that overflows: sweep at Now=10 removes "a", then put b,c,d -> 3 > 2.
	if _, e := x.Apply(Batch{Now: 10, Ops: []Op{{Put, "b", 20}, {Put, "c", 20}, {Put, "d", 20}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if after.Now != before.Now || after.Generation != before.Generation ||
		after.NextRevision != before.NextRevision || len(after.Entries) != 1 || after.Entries[0].Key != "a" {
		t.Fatalf("rollback failed: %+v -> %+v", before, after)
	}
}

func TestSweepBoundaryAndEmptyBatch(t *testing.T) {
	// Put with ExpiresAt <= Now is structurally rejected, so seed via earlier time.
	y, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	_, _ = y.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}})
	// Empty batch: generation and clock unchanged.
	r, e := y.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 1 || y.Snapshot().Now != 1 || len(y.Snapshot().Entries) != 2 {
		t.Fatal(e, r, y.Snapshot())
	}
	// Sweep at Now=5 removes only ExpiresAt<=5.
	if _, e = y.Apply(Batch{Now: 5, Ops: []Op{{Touch, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := y.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s)
	}
	gone, e := y.Expire(9)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e = y.Expire(8); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = y.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _, _, _ = x.Preview(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_ = x.ValidateBatch(Batch{Now: n, Ops: []Op{{Put, k, n + 1}}})
				_ = x.Stats()
				_ = x.Snapshot()
				if n%5 == 0 {
					_, _ = x.Clone()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries != 16 || s.Generation < 16 {
		t.Fatalf("%+v", s)
	}
}
