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
	cases := []struct {
		name string
		op   Op
	}{
		{"unknown kind", Op{Kind: 0, Resource: "a"}},
		{"kind too large", Op{Kind: 99, Resource: "a"}},
		{"empty resource", Op{Kind: Add}},
		{"bad char", Op{Kind: Add, Resource: "a?b"}},
		{"space", Op{Kind: Add, Resource: "a b"}},
		{"non-ascii", Op{Kind: Add, Resource: "é"}},
		{"too long", Op{Kind: Add, Resource: "0123456789abc"}},
		{"add extra owner", Op{Kind: Add, Resource: "a", Owner: "o"}},
		{"add extra expiry", Op{Kind: Add, Resource: "a", ExpiresAt: 9}},
		{"remove extra owner", Op{Kind: Remove, Resource: "a", Owner: "o"}},
		{"acquire no owner", Op{Kind: Acquire, Resource: "a", ExpiresAt: 9}},
		{"acquire bad owner", Op{Kind: Acquire, Resource: "a", Owner: "o!", ExpiresAt: 9}},
		{"acquire expiry at now", Op{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 0}},
		{"acquire expiry before now", Op{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: -1}},
		{"renew expiry at now", Op{Kind: Renew, Resource: "a", Owner: "o", ExpiresAt: 0}},
		{"release no owner", Op{Kind: Release, Resource: "a"}},
		{"release extra expiry", Op{Kind: Release, Resource: "a", Owner: "o", ExpiresAt: 3}},
	}
	for _, c := range cases {
		if _, e := p.Apply(Batch{Ops: []Op{c.op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%s: %v", c.name, e)
		}
	}
	if _, e := p.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := p.Snapshot(); s.Now != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestValidNameBoundaries(t *testing.T) {
	p := pool(t)
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "aA0._/-01234"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "0123456789ab"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationWholeBatchBeforeState(t *testing.T) {
	p := pool(t)
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}}); e != nil {
		t.Fatal(e)
	}
	before := p.Snapshot()
	// Second op is structurally invalid; must fail even though first op is fine.
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "b"}, {Kind: Add, Resource: "bad!"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, p.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestTimeMonotonic(t *testing.T) {
	p := pool(t)
	if _, e := p.Apply(Batch{Now: 10}); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Now: 10}); e != nil {
		t.Fatal(e) // equal is allowed
	}
	if _, e := p.Apply(Batch{Now: 9}); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := p.Expire(9); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	if _, e := p.Expire(-1); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if p.Snapshot().Now != 10 {
		t.Fatal("time moved")
	}
}

func TestCapacityRollback(t *testing.T) {
	p := pool(t) // MaxResources 3
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}, {Kind: Add, Resource: "b"}, {Kind: Add, Resource: "c"}}}); e != nil {
		t.Fatal(e)
	}
	before := p.Snapshot()
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "d"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, p.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Remove-then-add within one batch stays within capacity.
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Remove, Resource: "a"}, {Kind: Add, Resource: "d"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDuplicateAddAndMissingOps(t *testing.T) {
	p := pool(t)
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Remove, Resource: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Acquire, Resource: "zz", Owner: "o", ExpiresAt: 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Renew, Resource: "a", Owner: "o", ExpiresAt: 5}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Ops: []Op{{Kind: Release, Resource: "a", Owner: "o"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestBusyOwnerAndReleaseFlow(t *testing.T) {
	p := pool(t)
	_, e := p.Apply(Batch{Ops: []Op{
		{Kind: Add, Resource: "a"},
		{Kind: Acquire, Resource: "a", Owner: "x", ExpiresAt: 10},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Apply(Batch{Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "y", ExpiresAt: 20}}}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	if _, e = p.Apply(Batch{Ops: []Op{{Kind: Renew, Resource: "a", Owner: "y", ExpiresAt: 20}}}); !errors.Is(e, ErrOwner) {
		t.Fatal(e)
	}
	if _, e = p.Apply(Batch{Ops: []Op{{Kind: Release, Resource: "a", Owner: "y"}}}); !errors.Is(e, ErrOwner) {
		t.Fatal(e)
	}
	x, e := p.Apply(Batch{Ops: []Op{{Kind: Release, Resource: "a", Owner: "x"}}})
	if e != nil || len(x.Changed) != 0 {
		t.Fatalf("%+v %v", x, e)
	}
	if len(p.Snapshot().Leases) != 0 {
		t.Fatal("lease not released")
	}
}

func TestExpiredLeaseSemantics(t *testing.T) {
	p := pool(t)
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}, {Kind: Acquire, Resource: "a", Owner: "x", ExpiresAt: 5}}})
	// At Now=5 the lease is expired: Renew/Release report not found, Remove reports busy.
	if _, e := p.Apply(Batch{Now: 5, Ops: []Op{{Kind: Renew, Resource: "a", Owner: "x", ExpiresAt: 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Now: 5, Ops: []Op{{Kind: Release, Resource: "a", Owner: "x"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := p.Apply(Batch{Now: 5, Ops: []Op{{Kind: Remove, Resource: "a"}}}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	// Expired lease still present until Acquire/Expire.
	if len(p.Snapshot().Leases) != 1 {
		t.Fatal("expired lease vanished without commit")
	}
	// Failed acquire must not remove the expired lease.
	if _, e := p.Apply(Batch{Now: 5, Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "y", ExpiresAt: 8}, {Kind: Remove, Resource: "a"}}}); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	if len(p.Snapshot().Leases) != 1 {
		t.Fatal("rolled-back acquire removed expired lease")
	}
	// Successful acquire replaces it.
	x, e := p.Apply(Batch{Now: 5, Ops: []Op{{Kind: Acquire, Resource: "a", Owner: "y", ExpiresAt: 8}}})
	if e != nil || x.Changed[0].Owner != "y" {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestExpireGenerationAndSorting(t *testing.T) {
	p := pool(t)
	_, _ = p.Apply(Batch{Ops: []Op{
		{Kind: Add, Resource: "c"}, {Kind: Add, Resource: "a"}, {Kind: Add, Resource: "b"},
		{Kind: Acquire, Resource: "c", Owner: "o", ExpiresAt: 4},
		{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 4},
		{Kind: Acquire, Resource: "b", Owner: "o", ExpiresAt: 9},
	}})
	g := p.Snapshot().Generation
	gone, e := p.Expire(4)
	if e != nil || len(gone) != 2 || gone[0].Resource != "a" || gone[1].Resource != "c" {
		t.Fatalf("%+v %v", gone, e)
	}
	if p.Snapshot().Generation != g+1 {
		t.Fatal("generation not bumped")
	}
	// Expire with nothing expired keeps generation and returns empty.
	gone, e = p.Expire(5)
	if e != nil || len(gone) != 0 || p.Snapshot().Generation != g+1 {
		t.Fatalf("%+v %v", gone, e)
	}
	if p.Snapshot().Now != 5 {
		t.Fatal("expire did not advance time")
	}
}

func TestGenerationAndRevisionCounters(t *testing.T) {
	p := pool(t)
	x, _ := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}}})
	if x.Generation != 1 || x.Revision != 1 {
		t.Fatalf("%+v", x)
	}
	s := p.Snapshot()
	if s.Generation != 1 || s.NextRevision != 2 {
		t.Fatalf("%+v", s)
	}
	// Failed batch must not advance counters.
	_, _ = p.Apply(Batch{Ops: []Op{{Kind: Remove, Resource: "zz"}}})
	if s2 := p.Snapshot(); s2.Generation != 1 || s2.NextRevision != 2 {
		t.Fatalf("%+v", s2)
	}
	// Empty batch advances time only.
	x, _ = p.Apply(Batch{Now: 7})
	if x.Generation != 1 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatalf("%+v", x)
	}
}

func TestChangedSortingAndDedup(t *testing.T) {
	p := pool(t)
	x, e := p.Apply(Batch{Ops: []Op{
		{Kind: Add, Resource: "b"}, {Kind: Add, Resource: "a"},
		{Kind: Acquire, Resource: "b", Owner: "o", ExpiresAt: 9},
		{Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 9},
		{Kind: Renew, Resource: "b", Owner: "o", ExpiresAt: 10},
		{Kind: Renew, Resource: "b", Owner: "o", ExpiresAt: 11},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatalf("%+v %v", x, e)
	}
	if x.Changed[0].Resource != "a" || x.Changed[1].Resource != "b" || x.Changed[1].ExpiresAt != 11 {
		t.Fatalf("%+v", x.Changed)
	}
}

func TestReturnedSlicesIsolated(t *testing.T) {
	p := pool(t)
	x, _ := p.Apply(Batch{Ops: []Op{{Kind: Add, Resource: "a"}, {Kind: Acquire, Resource: "a", Owner: "o", ExpiresAt: 9}}})
	x.Changed[0].Owner = "hacked"
	s1 := p.Snapshot()
	s1.Resources[0] = "hacked"
	s1.Leases[0].Owner = "hacked"
	s2 := p.Snapshot()
	if s2.Resources[0] != "a" || s2.Leases[0].Owner != "o" {
		t.Fatal("returned slices alias internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	p, _ := New(Options{MaxResources: 128, MaxNameBytes: 16, MaxOwnerBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := fmt.Sprintf("res-%02d", i)
			o := fmt.Sprintf("own-%02d", i)
			for n := int64(1); n <= 20; n++ {
				_, _ = p.Apply(Batch{Now: n, Ops: []Op{{Kind: Add, Resource: r}}})
				_, _ = p.Apply(Batch{Now: n, Ops: []Op{{Kind: Acquire, Resource: r, Owner: o, ExpiresAt: n + 100}}})
				_, _ = p.Apply(Batch{Now: n, Ops: []Op{{Kind: Renew, Resource: r, Owner: o, ExpiresAt: n + 200}}})
				_, _ = p.Apply(Batch{Now: n, Ops: []Op{{Kind: Release, Resource: r, Owner: o}}})
				_, _ = p.Expire(n)
				_ = p.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := p.Snapshot()
	if len(s.Resources) != 32 {
		t.Fatal(len(s.Resources))
	}
	if s.Generation == 0 || s.NextRevision <= 1 {
		t.Fatalf("%+v", s)
	}
}
