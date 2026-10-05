package resourcelease144

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1}, {1, 0}, {-1, 8}, {8, -1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestKeyValidation(t *testing.T) {
	x := table(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "toolongkey", "é"}
	for _, k := range bad {
		if _, err := x.Apply(Batch{Ops: []Op{{Put, k, 1}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("key %q: %v", k, err)
		}
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Put, "ok_key-1", 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Ops: []Op{{Kind(0), "a", 1}, {Kind(99), "b", 1}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown kind accepted")
	}
}

func TestRollbackCapacity(t *testing.T) {
	x, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8})
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 100}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// "a" expires at Now=2, but the batch still overflows -> full rollback.
	_, err := x.Apply(Batch{Now: 2, Ops: []Op{{Put, "b", 5}, {Put, "c", 5}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("state changed after failed batch: %+v", got)
	}
	// Touch of missing key also rolls back revision allocation.
	if _, err := x.Apply(Batch{Now: 3, Ops: []Op{{Put, "b", 5}, {Touch, "ghost", 6}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := x.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("state changed after not-found batch: %+v", got)
	}
}

func TestExpireBoundaryAndMonotonic(t *testing.T) {
	x := table(t)
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 5}, {Put, "b", 6}}}); err != nil {
		t.Fatal(err)
	}
	gone, err := x.Expire(5)
	if err != nil || len(gone) != 1 || gone[0].Key != "a" {
		t.Fatal(err, gone)
	}
	if _, err := x.Expire(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := x.Apply(Batch{Now: 0, Ops: []Op{{Put, "c", 9}}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	x := table(t)
	r, err := x.Apply(Batch{Now: 0})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := x.Apply(Batch{Now: 1, Ops: []Op{{Put, "a", 9}}}); err != nil {
		t.Fatal(err)
	}
	r, err = x.Apply(Batch{Now: 2})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestConcurrentApplyExpireSnapshot(t *testing.T) {
	x, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("k-%d", i)
			for n := int64(1); n <= 50; n++ {
				_, _ = x.Apply(Batch{Now: n, Ops: []Op{{Put, key, n + 10}}})
				_, _ = x.Expire(n)
				_ = x.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := x.Snapshot()
	if len(s.Entries) > 128 {
		t.Fatal("capacity exceeded")
	}
	for i := 1; i < len(s.Entries); i++ {
		if s.Entries[i-1].Key >= s.Entries[i].Key {
			t.Fatal("snapshot not sorted")
		}
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, _ := New(Options{MaxEntries: 256, MaxKeyBytes: 16})
	pol, err := NewPolicy(2, []string{"alice", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := NewCoordinator(core, pol)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			for n := 0; n < 25; n++ {
				_, _ = coord.Apply(actor, Batch{Now: int64(n), Ops: []Op{{Put, fmt.Sprintf("k-%d-%d", i, n), 1000}}})
			}
		}()
	}
	wg.Wait()
	ds := coord.Decisions()
	if len(ds) != 200 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("gap in audit sequence at %d: %+v", i, d)
		}
	}
	ds[0].Actor = "mutated"
	if coord.Decisions()[0].Actor == "mutated" {
		t.Fatal("decision slice aliases internal state")
	}
	if err := pol.ReplaceActors([]string{"carol"}); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.Apply("alice", Batch{Ops: []Op{{Put, "z", 1}}}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestPolicyRejectsInvalidConfig(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	p, err := NewPolicy(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := p.Authorize("nobody", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
}
