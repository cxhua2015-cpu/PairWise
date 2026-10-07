package expirytable344

import (
	"errors"
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
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "中文", "toolongkey", "a+b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "z-0_9", "12345678"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 100}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestStructuralBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	// Unknown kind and negative ExpiresAt are structural errors and win
	// over the time regression check.
	for _, op := range []Op{{Kind(0), "a", 1}, {Kind(9), "a", 1}, {Put, "a", -1}} {
		if _, e := x.Apply(Batch{Now: 4, Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoChange(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	before := x.Snapshot()
	r, e = x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
}

func TestNotFoundAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch sweeps "a" (ExpiresAt 2 <= Now 2) in the candidate, then
	// fails with ErrNotFound; the sweep and time must roll back.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("failed batch mutated state")
	}
}

func TestCapacityRollback(t *testing.T) {
	x, e := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}, {Put, "b", 50}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 60}, {Put, "d", 60}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
	// Expiry sweep in the same batch frees room: a,b expire at 50 <= 50.
	r, e := x.Apply(Batch{Now: 50, Ops: []Op{{Put, "c", 60}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "c" || s.Now != 50 {
		t.Fatal(s)
	}
}

func TestExpireClosedBoundary(t *testing.T) {
	x := table(t)
	_, e := x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}, {Put, "c", 5}}})
	if e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if s := x.Snapshot(); len(s.Entries) != 1 || s.Entries[0].Key != "c" || s.Now != 4 {
		t.Fatal(s)
	}
	// Mutating the returned slice must not affect the table.
	gone[0].Key = "zz"
	if s := x.Snapshot(); s.Entries[0].Key != "c" {
		t.Fatal("snapshot aliased internal state")
	}
}

func TestRevisionMonotonicAcrossBatches(t *testing.T) {
	x := table(t)
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	r2, _ := x.Apply(Batch{Ops: []Op{{Touch, "a", 10}, {Delete, "a", 0}}})
	r3, _ := x.Apply(Batch{Ops: []Op{{Put, "b", 11}}})
	if r1.Revision != 1 || r2.Revision != 2 || r3.Revision != 3 {
		t.Fatal(r1, r2, r3)
	}
	if s := x.Snapshot(); s.Generation != 3 || s.NextRevision != 4 {
		t.Fatal(s)
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
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 10}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation == 0 || len(s.Entries) > 128 {
		t.Fatal(s.Generation, len(s.Entries))
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("entry %+v survives past now=%d", e, s.Now)
		}
	}
}
