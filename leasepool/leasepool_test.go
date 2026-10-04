package leasepool

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func mustRegistry(t *testing.T, o Options, cfgs []PoolConfig) *Registry {
	t.Helper()
	r, err := New(o, cfgs)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestExpirationRollbackOnFailure(t *testing.T) {
	r := registry(t)
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("cpu", "z", "a", 2, 3)}}); e != nil {
		t.Fatal(e)
	}
	before := r.Snapshot()
	// z expires at Now=3, but the batch fails: expiration must roll back.
	_, e := r.Apply(Batch{Now: 3, Ops: []Op{acq("gpu", "z2", "b", 1, 9), {Kind: Release, LeaseID: "nope"}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("state leaked: %+v", r.Snapshot())
	}
	if r.Snapshot().Pools[0].Used != 2 {
		t.Fatalf("usage leaked: %+v", r.Snapshot())
	}
}

func TestEmptyBatchTimeAdvanceNoGeneration(t *testing.T) {
	r := registry(t)
	res, e := r.Apply(Batch{Now: 5})
	if e != nil || res.Generation != 0 || len(res.Expired) != 0 {
		t.Fatalf("r=%+v e=%v", res, e)
	}
	if s := r.Snapshot(); s.Now != 5 || s.Generation != 0 {
		t.Fatalf("s=%+v", s)
	}
	// Empty batch that expires something does bump generation.
	if _, e := r.Apply(Batch{Now: 6, Ops: []Op{acq("cpu", "x", "a", 1, 7)}}); e != nil {
		t.Fatal(e)
	}
	res, e = r.Apply(Batch{Now: 7})
	if e != nil || res.Generation != 2 || !reflect.DeepEqual(res.Expired, []string{"x"}) {
		t.Fatalf("r=%+v e=%v", res, e)
	}
}

func TestReleaseThenAcquireSameBatch(t *testing.T) {
	r := registry(t)
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("cpu", "a", "o", 10, 9)}}); e != nil {
		t.Fatal(e)
	}
	// Release frees capacity and the ID, so re-acquire in the same batch works.
	res, e := r.Apply(Batch{Now: 2, Ops: []Op{
		{Kind: Release, LeaseID: "a"},
		acq("cpu", "a", "o2", 10, 9),
	}})
	if e != nil || res.Generation != 2 {
		t.Fatalf("r=%+v e=%v", res, e)
	}
	s := r.Snapshot()
	if len(s.Leases) != 1 || s.Leases[0].Owner != "o2" || s.Pools[0].Used != 10 {
		t.Fatalf("s=%+v", s)
	}
}

func TestRenewAndExpiryBoundary(t *testing.T) {
	r := registry(t)
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("cpu", "a", "o", 1, 5)}}); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Apply(Batch{Now: 2, Ops: []Op{{Kind: Renew, LeaseID: "a", ExpiresAt: 10}}}); e != nil {
		t.Fatal(e)
	}
	// Renew of an expired lease fails: a expires exactly at ExpiresAt <= now.
	r2 := registry(t)
	if _, e := r2.Apply(Batch{Now: 1, Ops: []Op{acq("cpu", "b", "o", 1, 5)}}); e != nil {
		t.Fatal(e)
	}
	if _, e := r2.Apply(Batch{Now: 5, Ops: []Op{{Kind: Renew, LeaseID: "b", ExpiresAt: 9}}}); !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
	// At exactly ExpiresAt the lease is expired by Sweep.
	exp, e := r.Sweep(5)
	if e != nil || len(exp) != 0 {
		t.Fatalf("exp=%v e=%v", exp, e)
	}
	exp, e = r.Sweep(10)
	if e != nil || !reflect.DeepEqual(exp, []string{"a"}) {
		t.Fatalf("exp=%v e=%v", exp, e)
	}
}

func TestCapacityAndOverflowEdge(t *testing.T) {
	o := Options{MaxPools: 1, MaxLeases: 8, MaxNameBytes: 8, MaxOwnerBytes: 8}
	r := mustRegistry(t, o, []PoolConfig{{Name: "p", Capacity: math.MaxUint64}})
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("p", "a", "o", math.MaxUint64-1, 9)}}); e != nil {
		t.Fatal(e)
	}
	// Remaining capacity is 1; weight 2 must fail without uint64 overflow.
	if _, e := r.Apply(Batch{Now: 2, Ops: []Op{acq("p", "b", "o", 2, 9)}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if _, e := r.Apply(Batch{Now: 2, Ops: []Op{acq("p", "b", "o", 1, 9)}}); e != nil {
		t.Fatal(e)
	}
	if s := r.Snapshot(); s.Pools[0].Used != math.MaxUint64 {
		t.Fatalf("s=%+v", s)
	}
	// MaxLeases enforcement.
	small := mustRegistry(t, o, []PoolConfig{{Name: "p", Capacity: 100}})
	small.opts.MaxLeases = 1
	if _, e := small.Apply(Batch{Now: 1, Ops: []Op{acq("p", "a", "o", 1, 9)}}); e != nil {
		t.Fatal(e)
	}
	if _, e := small.Apply(Batch{Now: 2, Ops: []Op{acq("p", "b", "o", 1, 9)}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
}

func TestTimeMonotonicity(t *testing.T) {
	r := registry(t)
	if _, e := r.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	before := r.Snapshot()
	if _, e := r.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) {
		t.Fatalf("e=%v", e)
	}
	if _, e := r.Sweep(4); !errors.Is(e, ErrTime) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("state changed on time error")
	}
	// Equal time is allowed.
	if _, e := r.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationOrderBeforeTimeCheck(t *testing.T) {
	r := registry(t)
	if _, e := r.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	// Invalid input must be reported even when time also regresses.
	_, e := r.Apply(Batch{Now: 1, Ops: []Op{{Kind: OpKind(99), LeaseID: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	// Release with non-zero extra fields is malformed.
	if _, e := r.Apply(Batch{Now: 6, Ops: []Op{{Kind: Release, LeaseID: "x", Weight: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	// Renew requires ExpiresAt > Batch.Now.
	if _, e := r.Apply(Batch{Now: 6, Ops: []Op{{Kind: Renew, LeaseID: "x", ExpiresAt: 6}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	// Acquire with ExpiresAt <= now is malformed.
	if _, e := r.Apply(Batch{Now: 6, Ops: []Op{acq("cpu", "x", "o", 1, 6)}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
}

func TestConflictSameBatchAndUnknownPool(t *testing.T) {
	r := registry(t)
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("cpu", "a", "o", 1, 9), acq("gpu", "a", "o", 1, 9)}}); !errors.Is(e, ErrConflict) {
		t.Fatalf("e=%v", e)
	}
	if s := r.Snapshot(); len(s.Leases) != 0 || s.Generation != 0 {
		t.Fatalf("s=%+v", s)
	}
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("nope", "a", "o", 1, 9)}}); !errors.Is(e, ErrNotFound) {
		t.Fatalf("e=%v", e)
	}
}

func TestSnapshotStableSortedCopy(t *testing.T) {
	r := mustRegistry(t, Options{MaxPools: 4, MaxLeases: 16, MaxNameBytes: 16, MaxOwnerBytes: 8},
		[]PoolConfig{{Name: "b", Capacity: 4}, {Name: "a", Capacity: 4}, {Name: "c", Capacity: 4}})
	if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("c", "l2", "o", 1, 9), acq("a", "l1", "o", 1, 9), acq("b", "l0", "o", 1, 9)}}); e != nil {
		t.Fatal(e)
	}
	s := r.Snapshot()
	if s.Pools[0].Name != "a" || s.Pools[1].Name != "b" || s.Pools[2].Name != "c" {
		t.Fatalf("pools=%+v", s.Pools)
	}
	if s.Leases[0].LeaseID != "l0" || s.Leases[1].LeaseID != "l1" || s.Leases[2].LeaseID != "l2" {
		t.Fatalf("leases=%+v", s.Leases)
	}
	// Mutating the snapshot must not affect the registry.
	s.Leases[0].Weight = 999
	s.Pools[0].Used = 999
	if again := r.Snapshot(); again.Leases[0].Weight != 1 || again.Pools[0].Used != 1 {
		t.Fatalf("snapshot aliases state: %+v", again)
	}
}

func TestConcurrentMixed(t *testing.T) {
	r := mustRegistry(t, Options{MaxPools: 2, MaxLeases: 256, MaxNameBytes: 16, MaxOwnerBytes: 8},
		[]PoolConfig{{Name: "p", Capacity: 1000}})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("l-%02d", i)
			if _, e := r.Apply(Batch{Now: 1, Ops: []Op{acq("p", id, "o", 1, 100)}}); e != nil {
				t.Error(e)
				return
			}
			if _, e := r.Apply(Batch{Now: 1, Ops: []Op{{Kind: Renew, LeaseID: id, ExpiresAt: 200}}}); e != nil {
				t.Error(e)
			}
			_, _ = r.Sweep(1)
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Leases) != 32 || s.Pools[0].Used != 32 || s.Generation != 64 {
		t.Fatalf("s=%+v", s)
	}
}
