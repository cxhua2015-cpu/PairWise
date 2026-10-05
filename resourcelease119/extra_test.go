package resourcelease119

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

func TestStructuralValidationBeforeTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	bad := []Batch{
		{Now: 1, Ops: []Op{{Kind(0), "a", 1}}},
		{Now: 1, Ops: []Op{{Kind(9), "a", 1}}},
		{Now: 1, Ops: []Op{{Put, "", 1}}},
		{Now: 1, Ops: []Op{{Put, "Upper", 1}}},
		{Now: 1, Ops: []Op{{Put, "a b", 1}}},
		{Now: 1, Ops: []Op{{Put, "toolongkey", 1}}},
		{Now: 1, Ops: []Op{{Put, "a", -1}}},
	}
	for i, b := range bad {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestValidKeyAlphabet(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "az_0-9", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndNegative(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: -1, Ops: []Op{{Put, "a", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e) // equal Now is allowed
	}
}

func TestEvictionOnApplyAndRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 2, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "old", 2}, {Put, "keep", 100}}}); e != nil {
		t.Fatal(e)
	}
	// "old" (ExpiresAt 2 <= Now 3) is evicted in the candidate, then Put fails
	// capacity only if eviction did not happen; here eviction frees the slot.
	if _, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "new", 100}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "keep" || s.Entries[1].Key != "new" {
		t.Fatal(s.Entries)
	}
	// Failed batch must roll back eviction, time and revision together.
	before := x.Snapshot()
	if _, e := x.Apply(Batch{Now: 100, Ops: []Op{{Touch, "ghost", 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("rollback leaked candidate state")
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 100}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("capacity failure leaked state")
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r0, e := x.Apply(Batch{Now: 1}) // empty batch: no generation bump
	if e != nil || r0.Generation != 0 {
		t.Fatal(e, r0)
	}
	r1, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 50}, {Put, "b", 50}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(e, r1)
	}
	r2, e := x.Apply(Batch{Now: 2, Ops: []Op{{Delete, "a", 0}}})
	if e != nil || r2.Generation != 2 || r2.Revision != 2 {
		t.Fatal(e, r2) // Delete allocates no revision
	}
	s := x.Snapshot()
	if s.NextRevision != 3 || s.Now != 2 {
		t.Fatal(s)
	}
}

func TestExpireBoundaryAndOwnership(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "x", 4}, {Put, "y", 5}, {Put, "z", 6}}}); e != nil {
		t.Fatal(e)
	}
	gone, e := x.Expire(5)
	if e != nil || len(gone) != 2 || gone[0].Key != "x" || gone[1].Key != "y" {
		t.Fatal(e, gone)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "z" {
		t.Fatal("returned slice aliases internal state")
	}
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "z" {
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
			k := fmt.Sprintf("k-%02d", i)
			for n := int64(1); n <= 10; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, k, 1000}}})
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Touch, k, 2000}}})
				_ = x.Snapshot()
			}
		}()
	}
	w.Wait()
	s := x.Snapshot()
	if len(s.Entries) != 32 {
		t.Fatal(len(s.Entries))
	}
	for _, e := range s.Entries {
		if e.ExpiresAt != 2000 || e.Revision == 0 || e.Revision >= s.NextRevision {
			t.Fatal(e)
		}
	}
}
