package expirytable379

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "z-0_", "12345678"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestBatchValidatedBeforeState(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 5}, {Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
	}
	// A structurally invalid op anywhere aborts before any state read.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok", 5}, {Kind(7), "bad", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpiryBeforeOps(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 (closed boundary), so Touch misses it.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "a" || s.Now != 1 {
		t.Fatalf("%+v", s)
	}
	// Put with ExpiresAt <= Now survives until the next batch/Expire.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 3}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatal(e, gone)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	r, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	if e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Second Put would exceed capacity; expiry of "a" at Now=2 must roll back too.
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	after := x.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("%+v != %+v", before, after)
	}
	if x.Snapshot().Generation != r.Generation {
		t.Fatal("generation changed on failure")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r0, e := x.Apply(Batch{Now: 0})
	if e != nil || r0.Generation != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	r2, _ := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "b", 10}}})
	if r1.Generation != 1 || r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r1, r2)
	}
	// Empty batch does not bump generation even at a newer time.
	r3, e := x.Apply(Batch{Now: 5})
	if e != nil || r3.Generation != 2 {
		t.Fatal(e, r3)
	}
}

func TestExpireMonotonicAndOrder(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 2}, {Put, "a", 2}, {Put, "b", 5}}})
	gone, e := x.Expire(2)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "c" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "z", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mut"
	s.Entries[0].ExpiresAt = 0
	again := x.Snapshot()
	if again.Entries[0].Key != "a" || again.Entries[0].ExpiresAt != 9 {
		t.Fatalf("%+v", again.Entries[0])
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
			k := fmt.Sprintf("key-%d", i)
			for now := int64(0); now < 50; now++ {
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}})
				_, _ = x.Apply(Batch{Now: now, Ops: []Op{{Touch, k, now + 200}}})
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 || s.NextRevision == 0 {
		t.Fatalf("%+v", s)
	}
	for _, e := range s.Entries {
		if e.ExpiresAt <= s.Now {
			t.Fatalf("stale entry %+v at now=%d", e, s.Now)
		}
	}
}
