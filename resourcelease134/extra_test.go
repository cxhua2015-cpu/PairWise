package resourcelease134

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 3})
	bad := []string{"", "AB", "a b", "toolong", "a.b", "é"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a_b-1", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("4-byte key should exceed MaxKeyBytes=3")
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a_b", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundaryAndMonotonic(t *testing.T) {
	x, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e = x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e = x.Apply(Batch{Now: 4, Ops: []Op{{Put, "c", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEvictionBeforeApplyAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=3, freeing capacity for "b".
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	// Failed batch must roll back eviction, time and revision.
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 10, Ops: []Op{{Put, "c", 1}, {Put, "d", 1}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "b", 20}}}); e != nil {
		t.Fatal(e)
	}
	if got := x.Snapshot().Entries[0]; got.ExpiresAt != 20 || got.Revision != 3 {
		t.Fatal(got)
	}
}

func TestNotFoundAndEmptyBatch(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "nope", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "nope", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || x.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 5}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	core, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	pol, _ := NewPolicy(2, []string{"a", "b"})
	coord, _ := NewCoordinator(core, pol)
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := string(rune('a' + i%3)) // "c" is denied
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			_, _ = coord.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Put, k, 100}}})
			_ = core.Snapshot()
			_, _ = core.Expire(int64(i % 3))
			_ = coord.Decisions()
			_ = pol.ReplaceActors([]string{"a", "b", "c"})
		}()
	}
	w.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence not contiguous")
		}
	}
}
