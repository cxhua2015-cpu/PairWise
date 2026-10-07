package expirytable349

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {3, -2}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a+b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeAndBackwardTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestEvictionBeforeOps(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at 2 <= Now=2 and is evicted before Touch runs.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch rolls back: time and entries are unchanged.
	s := x.Snapshot()
	if s.Now != 1 || len(s.Entries) != 2 {
		t.Fatalf("%+v", s)
	}
	// A successful batch evicts "a" via the closed boundary.
	if _, e := x.Apply(Batch{Now: 2}); e != nil {
		t.Fatal(e)
	}
	s = x.Snapshot()
	if s.Now != 2 || len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatalf("%+v", s)
	}
}

func TestNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "a", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "a", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Evicts "a" but still ends over capacity; everything must roll back.
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("state changed: %+v", x.Snapshot())
	}
}

func TestErrorRollbackRevisionAndTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Delete, "missing", 0}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("state changed: %+v", x.Snapshot())
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 4})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if g := x.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestExpireClosedBoundaryAndResult(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	gone, e = x.Expire(5)
	if e != nil || len(gone) != 0 {
		t.Fatal(e, gone)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	s.Entries[0].ExpiresAt = 1
	gone, _ := x.Expire(9)
	if len(gone) != 1 || gone[0].Key != "a" || gone[0].ExpiresAt != 9 {
		t.Fatal(gone)
	}
	gone[0].Key = "zz"
	if k := x.Snapshot().Entries; len(k) != 0 {
		t.Fatal(k)
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
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_ = x.Snapshot()
				_, _ = x.Expire(n)
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
}
