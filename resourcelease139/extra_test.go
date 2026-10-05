package resourcelease139

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	for _, k := range []string{"", "Bad", "key!", "toolongkey", "中文"} {
		if _, e := x.Apply(Batch{Ops: []Op{{Put, k, 9}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, e)
		}
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 9}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Apply(Batch{Ops: []Op{{Kind(99), "a", 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExpiryBoundaryAndRollback(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); e != nil {
		t.Fatal(e)
	}
	// Closed boundary: ExpiresAt <= Now is evicted inside Apply.
	if _, e := x.Apply(Batch{Now: 5, Ops: []Op{{Put, "c", 9}}}); e != nil {
		t.Fatal(e)
	}
	s := x.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].Key != "b" || s.Now != 5 {
		t.Fatal(s)
	}
	// Failed batch rolls back eviction, time and revisions.
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 6, Ops: []Op{{Touch, "missing", 9}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
	if _, e := x.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	_, _ = x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}})
	before := x.Snapshot()
	_, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 3})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, e = x.Apply(Batch{Now: 3, Ops: []Op{{Put, "a", 9}, {Put, "b", 9}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestConcurrentMixed(t *testing.T) {
	core, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	policy, _ := NewPolicy(2, []string{"alice", "bob"})
	coord, _ := NewCoordinator(core, policy)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			key := fmt.Sprintf("k-%d", i%8)
			_, _ = coord.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Put, key, 1000}}})
			_ = core.Snapshot()
			_, _ = core.Expire(int64(i))
			_ = coord.Decisions()
			if i%5 == 0 {
				_ = policy.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	w.Wait()
	ds := coord.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %+v", i, d)
		}
	}
}

func TestDecisionIsolation(t *testing.T) {
	core, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 8})
	policy, _ := NewPolicy(4, []string{"alice"})
	coord, _ := NewCoordinator(core, policy)
	_, _ = coord.Apply("alice", Batch{Ops: []Op{{Put, "a", 9}}})
	d1 := coord.Decisions()
	d1[0].Committed = false
	if !coord.Decisions()[0].Committed {
		t.Fatal("decisions alias internal log")
	}
	s := core.Snapshot()
	s.Entries[0].Key = "mutated"
	if core.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}
