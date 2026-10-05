package leasepool

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	p := pool(t)
	cases := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: Add, Resource: ""}}},
		{Ops: []Op{{Kind: Add, Resource: "has space"}}},
		{Ops: []Op{{Kind: Add, Resource: "bad?"}}},
		{Ops: []Op{{Kind: Add, Resource: "toolongname123"}}},
		{Ops: []Op{{Kind: Add, Resource: "a", Owner: "x"}}},
		{Ops: []Op{{Kind: Add, Resource: "a", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Remove, Resource: "a", Owner: "x"}}},
		{Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 0}}},
		{Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "bad owner", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Renew, Resource: "a", Owner: "o", ExpiresAt: 0}}},
		{Ops: []Op{{Kind: Release, Resource: "a", Owner: "o", ExpiresAt: 1}}},
		{Ops: []Op{{Kind: Kind(0), Resource: "a"}}},
		{Ops: []Op{{Kind: Kind(99), Resource: "a"}}},
	}
	for i, b := range cases {
		if _, e := p.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	// Valid names: letters, digits, dot, underscore, slash, hyphen.
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "aB0._/-"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationCoversWholeBatchBeforeTime(t *testing.T) {
	p := pool(t)
	if _, e := p.Apply(Batch{Now: 2}); e != nil {
		t.Fatal(e)
	}
	// Bad op appears after a good op; time also moved backwards. Validation wins.
	_, e := p.Apply(Batch{Now: 1, Ops: []Op{{Kind: Add, Resource: "ok"}, {Kind: Add, Resource: "ok", Owner: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Now: 1}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
}

func TestAddRemoveAcquireErrors(t *testing.T) {
	p := pool(t)
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Remove, Resource: "nope"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Acquire, Resource: "nope", Owner: "o", ExpiresAt: 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Renew, Resource: "nope", Owner: "o", ExpiresAt: 1}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Release, Resource: "nope", Owner: "o"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}})
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 10}}})
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "x", ExpiresAt: 20}}}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Renew, Resource: "a", Owner: "x", ExpiresAt: 20}}}); !errors.Is(e, ErrOwner) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Release, Resource: "a", Owner: "x"}}}); !errors.Is(e, ErrOwner) {
		t.Fatal(e)
	}
}

func TestExpiredLeaseRenewReleaseNotFound(t *testing.T) {
	p := pool(t)
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}, {Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 2}}})
	// At Now=2 the lease is expired: Renew/Release report ErrNotFound even for the owner.
	if _, e := p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Renew, Resource: "a", Owner: "o", ExpiresAt: 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Release, Resource: "a", Owner: "o"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Expired lease still blocks Remove until Acquire/Expire clears it.
	if _, e := p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Remove, Resource: "a"}}}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	// Failed batches must not have removed the expired lease.
	if got := len(p.Snapshot().Leases); got != 1 {
		t.Fatal(got)
	}
	// Successful Acquire replaces and removes the stale lease.
	x, e := p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "n", ExpiresAt: 9}}})
	if e != nil || x.Changed[0].Owner != "n" {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	p, _ := New(Options{MaxResources: 2, MaxNameBytes: 8, MaxOwnerBytes: 8})
	// Transiently exceed capacity inside the batch; fine at the end.
	_, e := p.Apply(Batch{Ops: []Op{
		{Kind: Add, Resource: "a"}, {Kind: Add, Resource: "b"}, {Kind: Add, Resource: "c"},
		{Kind: Remove, Resource: "c"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeding at batch end fails and rolls everything back.
	before := p.Snapshot()
	_, e = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "c"}, {Kind: Add, Resource: "d"}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, p.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRollbackKeepsTimeGenerationRevision(t *testing.T) {
	p := pool(t)
	_, _ = p.Apply(Batch{Now: 3, Ops: []Op{{Kind: Add, Resource: "a"}}})
	before := p.Snapshot()
	_, e := p.Apply(Batch{Now: 5, Ops: []Op{{Kind: Add, Resource: "b"}, {Kind: Remove, Resource: "zz"}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, p.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
}

func TestChangedDedupSortedAndSurviving(t *testing.T) {
	p := pool(t)
	x, e := p.Apply(Batch{Ops: []Op{
		{Kind: Add, Resource: "b"}, {Kind: Add, Resource: "a"}, {Kind: Add, Resource: "c"},
		{Kind: Acquire, Resource: "b", Owner: "o", ExpiresAt: 10},
		{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 10},
		{Kind: Renew, Resource: "b", Owner: "o", ExpiresAt: 20},
		{Kind: Acquire, Resource: "c", Owner: "o", ExpiresAt: 10},
		{Kind: Release, Resource: "c", Owner: "o"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 2 || x.Changed[0].Resource != "a" || x.Changed[1].Resource != "b" {
		t.Fatalf("%+v", x.Changed)
	}
	if x.Changed[1].ExpiresAt != 20 {
		t.Fatal("renew not reflected")
	}
	// Released lease does not survive, so it is absent from Changed and Snapshot.
	if got := len(p.Snapshot().Leases); got != 2 {
		t.Fatal(got)
	}
}

func TestGenerationAndRevisionSemantics(t *testing.T) {
	p := pool(t)
	x, _ := p.Apply(Batch{Now: 1})
	if x.Generation != 0 || x.Revision != 0 || p.Snapshot().Generation != 0 {
		t.Fatalf("%+v", x)
	}
	x, _ = p.Apply(Batch{Now: 2, Ops: []Op{{Kind: Add, Resource: "a"}}})
	if x.Generation != 1 || x.Revision != 1 || p.Snapshot().NextRevision != 2 {
		t.Fatalf("%+v", x)
	}
	// Expire with nothing expired does not bump generation.
	if _, e := p.Expire(2); e != nil || p.Snapshot().Generation != 1 {
		t.Fatal(e)
	}
	_, _ = p.Apply(Batch{Now: 3, Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 4}}})
	gone, e := p.Expire(4)
	if e != nil || len(gone) != 1 || p.Snapshot().Generation != 3 {
		t.Fatalf("%v %v", gone, e)
	}
	// Expire allocates no revisions.
	if p.Snapshot().NextRevision != 3 {
		t.Fatal(p.Snapshot().NextRevision)
	}
}

func TestExpireNegativeAndMonotonic(t *testing.T) {
	p := pool(t)
	if _, e := p.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := p.Expire(5); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Expire(4); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if p.Snapshot().Now != 5 {
		t.Fatal(p.Snapshot().Now)
	}
}

func TestReturnedSlicesAreIsolated(t *testing.T) {
	p := pool(t)
	x, _ := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}, {Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 5}}})
	x.Changed[0].Owner = "mutated"
	s := p.Snapshot()
	s.Resources[0] = "mutated"
	s.Leases[0].Owner = "mutated"
	gone, _ := p.Expire(5)
	gone[0].Owner = "mutated"
	s2 := p.Snapshot()
	if s2.Resources[0] != "a" || len(s2.Leases) != 0 {
		t.Fatalf("%+v", s2)
	}
}

func TestConcurrentMixed(t *testing.T) {
	p, _ := New(Options{MaxResources: 128, MaxNameBytes: 16, MaxOwnerBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := fmt.Sprintf("r-%02d", i)
			_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: r}, {Kind: Acquire, Resource: r, Owner: "o", ExpiresAt: 10}}})
			_, _ = p.Expire(0)
			_ = p.Snapshot()
		}()
	}
	wg.Wait()
	s := p.Snapshot()
	if len(s.Resources) != 32 {
		t.Fatal(len(s.Resources))
	}
	for _, l := range s.Leases {
		if l.Owner != "o" {
			t.Fatalf("%+v", l)
		}
	}
}
