package resourcelease149

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptionsAndInput(t *testing.T) {
	if _, e := New(Options{MaxEntries: 0, MaxKeyBytes: 4}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	if _, e := New(Options{MaxEntries: 1, MaxKeyBytes: -1}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := table(t)
	bad := []Batch{
		{Now: -1, Ops: []Op{{Put, "a", 1}}},
		{Ops: []Op{{Kind(0), "a", 1}}},
		{Ops: []Op{{Kind(9), "a", 1}}},
		{Ops: []Op{{Put, "", 1}}},
		{Ops: []Op{{Put, "Bad", 1}}},
		{Ops: []Op{{Put, "waytoolong", 1}}},
		{Ops: []Op{{Put, "a", -1}}},
	}
	for i, b := range bad {
		if _, e := x.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Entries) != 0 {
		t.Fatal(s)
	}
}

func TestRollbackCapacityKeepsState(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "a", 100}}}); e != nil {
		t.Fatal(e)
	}
	before := x.Snapshot()
	// Second Put overflows final capacity: everything must roll back,
	// including expiry of "a" (ExpiresAt 100 > 3 so it survives anyway)
	// and the revision counter.
	_, e := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 50}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Touch of a missing key also rolls back.
	_, e = x.Apply(Batch{Now: 4, Ops: []Op{{Touch, "ghost", 9}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
}

func TestExpiryPurgesBeforeCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, e := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 2}}}); e != nil {
		t.Fatal(e)
	}
	// "a" expires at Now=2 (closed boundary), freeing capacity for "b".
	r, e := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 9}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(e, r)
	}
	s := x.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].Key != "b" {
		t.Fatal(s)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	x := table(t)
	r, e := x.Apply(Batch{Now: 5})
	if e != nil || r.Generation != 0 || x.Snapshot().Now != 5 {
		t.Fatal(e, r)
	}
}

func TestExpireMonotonic(t *testing.T) {
	x := table(t)
	if _, e := x.Apply(Batch{Now: 10}); e != nil {
		t.Fatal(e)
	}
	if _, e := x.Expire(9); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := x.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := table(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Put, "a", 9}}})
	s := x.Snapshot()
	s.Entries[0].Key = "mutated"
	if x.Snapshot().Entries[0].Key != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	p, _ := NewPolicy(4, []string{"alice", "bob"})
	c, _ := NewCoordinator(x, p)
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			actor := "alice"
			if i%2 == 1 {
				actor = "bob"
			}
			k := fmt.Sprintf("key-%d", i)
			_, _ = c.Apply(actor, Batch{Now: int64(i), Ops: []Op{{Put, k, 1000}}})
			_, _ = x.Expire(int64(i))
			_ = x.Snapshot()
			_ = c.Decisions()
			_ = p.Authorize(actor, 1)
		}()
	}
	w.Wait()
	ds := c.Decisions()
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap in audit sequence at %d: %+v", i, d)
		}
	}
}

func TestConcurrentPolicyReplace(t *testing.T) {
	x, _ := New(Options{MaxEntries: 64, MaxKeyBytes: 8})
	p, _ := NewPolicy(2, []string{"alice"})
	c, _ := NewCoordinator(x, p)
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(2)
		go func() {
			defer w.Done()
			_ = p.ReplaceActors([]string{fmt.Sprintf("actor-%d", i)})
		}()
		go func() {
			defer w.Done()
			_, _ = c.Apply(fmt.Sprintf("actor-%d", i), Batch{Ops: []Op{{Put, "k", 9}}})
		}()
	}
	w.Wait()
}
