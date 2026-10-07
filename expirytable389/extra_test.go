package expirytable389

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	bad := []string{"", "A", "a b", "a.b", "toolongkey", "é"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	for _, k := range []string{"a", "0", "-", "_", "abcd-12_"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKindAndNegativeExpiry(t *testing.T) {
x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(9), "a", 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNegativeNow(t *testing.T) {
x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 1}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestRollbackOnNotFoundAndCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch of a missing key must roll back the whole batch, including the
	// expiry of "a" (ExpiresAt 2 <= Now 2) and the revision bump.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Touch, "zz", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// Capacity overflow must roll back too.
	_, e = x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestGenerationAndRevision(t *testing.T) {
x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	r, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r)
	}
	// Empty batch: no generation bump, no state change.
	r, _ = x.Apply(Batch{Now: 5})
	if r.Generation != 1 || x.Snapshot().Now != 0 {
		t.Fatal(r, x.Snapshot())
	}
	r, _ = x.Apply(Batch{Now: 5, Ops: []Op{{Delete, "a", 0}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	s := x.Snapshot()
	if s.NextRevision != 3 || s.Now != 5 || s.Generation != 2 {
		t.Fatal(s)
	}
}

func TestApplyExpiresBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 2}}})
	// "a" expires at Now=2, freeing capacity for "b" in the same batch.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	// Touch on an entry expiring at this Now must fail: it is already gone.
	if _, e := x.Apply(Batch{Now: 9, Ops: []Op{{Touch, "b", 10}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestExpireMonotonicAndIsolation(t *testing.T) {
x, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 7}}})
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(e, gone)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Now != 3 {
		t.Fatal("expire did not advance now")
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "b" {
		t.Fatal("snapshot shares memory with table")
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
			for n := int64(1); n <= 20; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, n + 100}}})
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
			t.Fatal("expired entry survived", e, s.Now)
		}
	}
}
