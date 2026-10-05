package resourcelease154

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

func TestStructuralValidation(t *testing.T) {
	x := table(t)
	bad := [][]Op{
		{{Kind: 0, Key: "a"}},
		{{Kind: 99, Key: "a"}},
		{{Kind: Put, Key: ""}},
		{{Kind: Put, Key: "Upper"}},
		{{Kind: Put, Key: "bad key"}},
		{{Kind: Put, Key: "toolongkey"}},
		{{Kind: Put, Key: "a", ExpiresAt: -1}},
	}
	for _, ops := range bad {
		if _, e := x.Apply(Batch{Ops: ops}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("ops %+v: %v", ops, e)
		}
	}
	if _, e := x.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatalf("state changed: %+v", s)
	}
}

func TestValidKeyAlphabet(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a-z_09", 10}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEvictionRollbackOnCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	// a expires at 1; batch at Now=1 evicts a, then fails final capacity.
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 1}, {Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "c", 9}, {Put, "d", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatalf("eviction/time/revision not rolled back: %+v", x.Snapshot())
	}
}

func TestNotFoundRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 3}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Touch of a missing key must roll back the earlier Put in the same batch.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}, {Touch, "ghost", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "ghost", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestTouchExpiredIsNotFound(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// At Now=2 the entry (ExpiresAt=2) is evicted before ops run.
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 0 {
		t.Fatalf("empty batch changed state: %+v %+v", r, x.Snapshot())
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
	if g := x.Snapshot().Generation; g != 2 {
		t.Fatalf("generation=%d", g)
	}
}

func TestExpireBoundaryAndClock(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 3}, {Put, "b", 4}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(3)
	if e != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatalf("closed boundary: %v %v", e, gone)
	}
	if _, e := x.Expire(2); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	gone, e = x.Expire(4)
	if e != nil || len(gone) != 1 || gone[0].Key != "b" {
		t.Fatalf("%v %v", e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 0 {
		t.Fatal(n)
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
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 1; j <= 10; j++ {
				now := int64(j)
				if _, e := x.Apply(Batch{Now: now, Ops: []Op{{Put, k, now + 100}}}); e != nil {
					continue // time may have moved past us; that is fine
				}
				_, _ = x.Expire(now)
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity violated")
	}
	seen := map[string]bool{}
	for _, e := range s.Entries {
		if seen[e.Key] {
			t.Fatal("duplicate key")
		}
		seen[e.Key] = true
		if e.ExpiresAt <= s.Now {
			t.Fatalf("entry %+v should have been expired at now=%d", e, s.Now)
		}
	}
}
