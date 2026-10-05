package devicelease

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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a/b"}
	for _, k := range bad {
		_, e := x.Apply(Batch{Ops: []Op{{Put, k, 1}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	x2, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8})
	for _, k := range []string{"a", "a-b_c", "01234567", "z9_-"} {
		if _, e := x2.Apply(Batch{Ops: []Op{{Put, k, 1}}}); e != nil {
			t.Fatalf("key %q: %v", k, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	x := table(t)
	for _, k := range []Kind{0, 4, 255} {
		if _, e := x.Apply(Batch{Ops: []Op{{Kind: k, Key: "a", ExpiresAt: 1}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestNegativeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", -2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestEvictionOnApply(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 5}}})
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}})
	if e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
	if r.Generation != 2 {
		t.Fatal(r)
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

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	b := x.Snapshot()
	// would evict "a" (ExpiresAt 2 <= Now 2) then put two keys -> over capacity
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}})
	b := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "missing", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	if x.Snapshot().Now != 3 {
		t.Fatal("now not advanced")
	}
	r, _ = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestExpireBoundaryAndOwnership(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 4}, {Put, "b", 5}, {Put, "c", 6}}})
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "c" {
		t.Fatal(s.Entries)
	}
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "c" {
		t.Fatal("snapshot shares internal state")
	}
}

func TestRevisionMonotonicAcrossBatches(t *testing.T) {
	x := table(t)
	r1, _ := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	r2, _ := x.Apply(Batch{Ops: []Op{{Touch, "a", 10}}})
	if r1.Revision != 1 || r2.Revision != 2 {
		t.Fatal(r1, r2)
	}
	s := x.Snapshot()
	if s.NextRevision != 3 || s.Entries[0].Revision != 2 || s.Entries[0].ExpiresAt != 10 {
		t.Fatal(s)
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
}
