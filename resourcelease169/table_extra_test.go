package resourcelease169

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 8}, {3, 0}, {-1, 8}, {3, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	bad := []Batch{
		{Now: 1, Ops: []Op{{Kind: 0, Key: "a", ExpiresAt: 9}}},
		{Now: 1, Ops: []Op{{Kind: 99, Key: "a", ExpiresAt: 9}}},
		{Now: 1, Ops: []Op{{Put, "", 9}}},
		{Now: 1, Ops: []Op{{Put, "A", 9}}},
		{Now: 1, Ops: []Op{{Put, "bad key", 9}}},
		{Now: 1, Ops: []Op{{Put, "toolongkey", 9}}},
		{Now: 1, Ops: []Op{{Put, "a", -1}}},
		{Now: 1, Ops: []Op{{Delete, "bad?", 0}}},
		{Now: -1, Ops: []Op{{Put, "a", 9}}},
	}
	for _, b := range bad {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "ok", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	x := table(t)
	b := x.Snapshot()
	r, e := x.Apply(Batch{Now: 7})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatal(e, r)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
}

func TestEvictBeforeOpsAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "keep", 10}}}); e != nil {
		t.Fatal(e)
	}
	// "old" (ExpiresAt 2 <= Now 3) is evicted in candidate state, freeing capacity.
	r, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "new", 10}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "keep" || s.Entries[1].Key != "new" || s.Now != 3 {
		t.Fatal(s)
	}
	// Failing batch rolls back eviction, time and revision together.
	if _, e := x.Apply(Batch{Now: 20, Ops: []Op{{Touch, "ghost", 30}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(s, x.Snapshot()) {
		t.Fatal("failed batch leaked candidate state")
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("capacity failure leaked candidate state")
	}
}

func TestDeleteNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestExpireBoundaryAndTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	s.Entries[0].ExpiresAt = 0
	gone, _ := x.Expire(9)
	gone[0].Key = "mut"
	again := x.Snapshot()
	if len(again.Entries) != 0 {
		t.Fatal(again)
	}
	y := table(t)
	_, _ = y.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	if y.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliased internal state")
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
			if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 50}}}); e != nil {
				t.Error(e)
				return
			}
			if _, e := x.Apply(Batch{Ops: []Op{{Touch, k, 60}}}); e != nil {
				t.Error(e)
			}
			_, _ = x.Expire(0)
			_ = x.Snapshot()
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 || s.Generation != 64 {
		t.Fatal(s.Generation, len(s.Entries))
	}
	seen := map[uint64]bool{}
	for _, e := range s.Entries {
		if e.Revision == 0 || seen[e.Revision] {
			t.Fatal("duplicate or zero revision", e)
		}
		seen[e.Revision] = true
	}
}
