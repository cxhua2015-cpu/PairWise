package sessiontable

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "A", "a b", "a.b", "toolongkey", "中文"} {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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

func TestClosedBoundary(t *testing.T) {
	x := table(t)
	// Entry expiring exactly at Now is collected before ops run, so Touch fails.
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	_, e := x.Apply(Batch{Now: 5, Ops: []Op{{Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Failed batch rolls back the eviction too: "a" survives.
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	// A successful batch at Now=5 collects it via the closed boundary.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	for _, en := range x.Snapshot().Entries {
		if en.Key == "a" {
			t.Fatal("a should have expired at boundary")
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 2 {
		t.Fatal(e, r)
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = x.Apply(Batch{Now: 4})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r1, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed: %+v -> %+v", before, after)
	}
	// Revision was rolled back: next successful Put reuses it.
	r2, _ := x.Apply(Batch{Now: 3, Ops: []Op{{Delete, "a", 0}, {Put, "b", 100}}})
	if r2.Revision != r1.Revision+1 {
		t.Fatal(r1, r2)
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	before := x.Snapshot()
	// Put succeeds but later Touch fails: everything rolls back.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}, {Touch, "zz", 5}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 10, Ops: []Op{{Put, "a", 20}}})
	if _, e := x.Expire(9); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone, e := x.Expire(20)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
}

func TestExpireAdvancesNow(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 7}}})
	if _, e := x.Expire(8); e != nil {
		t.Fatal(e)
	}
	if x.Snapshot().Now != 8 {
		t.Fatal(x.Snapshot().Now)
	}
	if _, e := x.Apply(Batch{Now: 7}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "zz"
	s.Entries[0].ExpiresAt = 1
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
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
			for n := int64(0); n < 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal(len(s.Entries))
	}
	seen := map[string]bool{}
	for _, e := range s.Entries {
		if seen[e.Key] {
			t.Fatal("duplicate key")
		}
		seen[e.Key] = true
	}
}
