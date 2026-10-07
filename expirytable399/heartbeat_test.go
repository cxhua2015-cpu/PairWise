package expirytable399

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

func TestInvalidInput(t *testing.T) {
	x := table(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a"}}},
		{Ops: []Op{{Kind: 99, Key: "a"}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "A", 1}}},
		{Ops: []Op{{Put, "has space", 1}}},
		{Ops: []Op{{Put, "toolongkey", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
	}
	for _, b := range cases {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestMonotonicTime(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "a", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Now: 4, Ops: []Op{{Put, "b", 9}}}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	// Equal Now is allowed.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "b", 9}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCandidateEvictionBeforeOps(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 (closed interval), freeing capacity for "b".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" || s.Now != 2 {
		t.Fatal(s)
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Put, "c", 9}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Eviction of nothing expired, time, revisions all rolled back.
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestRollbackNotFound(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}})
	before := x.Snapshot()
	// Now=3 evicts "a" in the candidate; touching it must fail and roll back
	// the eviction, the clock and the revision allocated to the earlier Put.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 9}, {Touch, "a", 9}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Delete, "missing", 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevision(t *testing.T) {
	x := table(t)
	r0, e := x.Apply(Batch{Now: 1}) // empty batch: no change
	if e != nil || r0.Generation != 0 || x.Snapshot().Now != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := x.Apply(Batch{Now: 2, Ops: []Op{{Touch, "a", 9}, {Delete, "b", 0}}})
	if r2.Generation != 2 || r2.Revision != 3 {
		t.Fatal(r2)
	}
	s := x.Snapshot()
	if s.NextRevision != 4 || len(s.Entries) != 1 || s.Entries[0].Revision != 3 {
		t.Fatal(s)
	}
}

func TestExpireClosedInterval(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 3}, {Put, "b", 4}, {Put, "c", 5}}})
	gone, e := x.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Key != "a" || gone[1].Key != "b" {
		t.Fatal(e, gone)
	}
	if n := len(x.Snapshot().Entries); n != 1 {
		t.Fatal(n)
	}
	gone[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key == "mutated" {
		t.Fatal("returned slice aliases internal state")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].ExpiresAt = 0
	if x.Snapshot().Entries[0].ExpiresAt != 9 {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i)
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
