package expirytable424

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("expire: %v %v", e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestNegativeAndUnknownKind(t *testing.T) {
	x := table(t)
	if e := x.ValidateBatch(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Kind: 9, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "A", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "toolongkey", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if e := x.ValidateBatch(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
}

func TestInvalidOptions(t *testing.T) {
	if _, e := New(Options{0, 1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{1, 0}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}

func TestCapacityRollbackRestoresAll(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 10}}})
	if e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// "a" is not expired at Now=3; adding "b" overflows final capacity.
	_, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 10}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed: %+v vs %+v", before, after)
	}
	if x.Stats().Now != 2 || x.Stats().NextRevision != r.Revision+1 {
		t.Fatalf("clocks changed: %+v", x.Stats())
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
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
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after ErrNotFound")
	}
}

func TestExpiryBeforeOps(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=3 (closed boundary), so Touch must fail with ErrNotFound.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch rolls back eviction too: "a" is still present.
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	if _, e := x.Apply(Batch{Now: 3}); e != nil {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 2 {
		t.Fatal("empty batch must still advance time")
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestExpireTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%02d", i)
			_, _ = x.Apply(Batch{Ops: []Op{{Put, k, 50}}})
			_, _, _, _ = x.Preview(Batch{Ops: []Op{{Touch, k, 60}}})
			_, _ = x.Clone()
			_ = x.Stats()
			_ = x.Snapshot()
			_ = x.ValidateBatch(Batch{Ops: []Op{{Delete, k, 0}}})
		}()
	}
	w.Wait()
	if _, e := x.Expire(60); e != nil {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
}
