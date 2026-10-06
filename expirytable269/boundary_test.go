package expirytable269

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestEvictionReclaimsCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at 2 <= Now=3, so the candidate eviction frees capacity.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
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
	// Evicting "a" would make room, but the batch fails on final capacity
	// only if it still overflows; here two puts into one slot must fail and
	// roll back the eviction, the clock, and the revisions together.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestNotFoundRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 10}, {Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after not-found failure")
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "ghost", 10}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestClosedBoundaryAndClock(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("put with ExpiresAt <= Now must be rejected", e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 6}, {Put, "b", 7}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(6)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(5); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if got := x.Snapshot(); got.Now != 0 || got.Generation != 0 {
		t.Fatal("empty batch mutated state", got)
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Delete, "a", 0}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestValidateBatchPure(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4})
	before := x.Snapshot()
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Upper", 1}}},
		{Ops: []Op{{Put, "toolong", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
		{Ops: []Op{{Touch, "a", 0}}},
	}
	for _, b := range cases {
		if e := x.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "a-1", 1}, {Delete, "a-1", 0}}}); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("ValidateBatch mutated state")
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
			k := fmt.Sprintf("k%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Stats()
				_ = x.Snapshot()
				if c, err := x.Clone(); err == nil {
					_ = c.Snapshot()
				}
			}
		}()
	}
	w.Wait()
	s := x.Stats()
	if s.Entries < 0 || s.Entries > 128 || s.NextRevision < 2 {
		t.Fatal(s)
	}
}
