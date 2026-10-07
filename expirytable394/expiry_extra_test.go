package expirytable394

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	// Unknown kind wins over the time check.
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Kind(99), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Whole batch validated before any state read: bad second op, no partial apply.
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 6, Ops: []Op{{Put, "b", 9}, {Put, "BAD KEY", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("state changed after invalid batch")
	}
}

func TestKeyRules(t *testing.T) {
	x := table(t) // MaxKeyBytes 8
	for _, k := range []string{"", "A", "a b", "a.b", "abcdefghi", "中文"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "z0-_", "12345678"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestClosedIntervalExpiry(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	// ExpiresAt == Now must be evicted by the next Apply.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	var keys []string
	for _, en := range x.Snapshot().Entries {
		keys = append(keys, en.Key)
	}
	if !reflect.DeepEqual(keys, []string{"b", "c"}) {
		t.Fatal(keys)
	}
	// Expire uses the same boundary.
	gone, e := x.Expire(6)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 3}}})
	if e != nil || r.Revision != 1 {
		t.Fatal(e, r)
	}
	b := x.Snapshot()
	// Final state would exceed capacity: evictions, time and revision roll back.
	_, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "missing", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("error did not roll back time/revision")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 0})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	r, e = x.Apply(Batch{Now: 2})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	if g := x.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Equal Now is allowed.
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); e != nil {
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
	gone, _ := x.Expire(9)
	gone[0].Key = "mutated"
	if x.Snapshot().Now != 9 {
		t.Fatal("expire did not advance time")
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
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 200}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatal(s)
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("stale entry survived: %+v now=%d", e, s.Now)
		}
	}
}
