package reservationlease

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongkey", "a+b"}
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

func TestUnknownKindAndNegativeExpiry(t *testing.T) {
	x := table(t)
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

func TestTimeMonotonicAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 10}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 10}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("state changed after time error")
	}
	if _, e := x.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	if x.Snapshot().Generation != 1 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestEvictionBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 (closed interval), freeing capacity for "c".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "c", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Entries[1].Key != "c" {
		t.Fatal(s.Entries)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	// Eviction of "a" would occur at Now=2, but final capacity still fails.
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("eviction/time/revision leaked after capacity failure")
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	b := x.Snapshot()
	if _, e := x.Apply(Batch{Ops: []Op{{Touch, "missing", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Partial batch: Put succeeds, then Touch fails -> Put rolled back too.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}, {Touch, "missing", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, x.Snapshot()) {
		t.Fatal("partial batch leaked")
	}
}

func TestTouchExpiredKeyNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestExpireClosedBoundaryAndOrder(t *testing.T) {
	x := table(t)
	_, e := x.Apply(Batch{Ops: []Op{{Put, "c", 4}, {Put, "a", 4}, {Put, "b", 5}}})
	if e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "c" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
}

func TestGenerationAndRevisionCounters(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}, {Put, "b", 9}, {Touch, "a", 10}}})
	if e != nil || r.Generation != 1 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || s.Generation != 1 {
		t.Fatal(s)
	}
	if s.Entries[0].Revision != 3 || s.Entries[1].Revision != 2 {
		t.Fatal(s.Entries)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	s.Entries[0].Key = "hacked"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot shares memory with table")
	}
	gone, _ := x.Expire(9)
	gone[0].Key = "hacked"
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "b", 9}}})
	for _, en := range x.Snapshot().Entries {
		if en.Key == "hacked" {
			t.Fatal("expire result shares memory with table")
		}
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
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, n + 100}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 || s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatal(s.Generation, s.NextRevision, len(s.Entries))
	}
}
