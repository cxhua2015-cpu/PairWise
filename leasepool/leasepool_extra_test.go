package leasepool

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func mustApply(t *testing.T, r *Registry, b Batch) Result {
	t.Helper()
	res, err := r.Apply(b)
	if err != nil {
		t.Fatalf("Apply(%+v) failed: %v", b, err)
	}
	return res
}

func TestExpiryRollbackKeepsUsageAndTime(t *testing.T) {
	r := registry(t)
	mustApply(t, r, Batch{Now: 1, Ops: []Op{acq("cpu", "soon", "a", 4, 5), acq("cpu", "late", "a", 3, 50)}})
	before := r.Snapshot()
	// At Now=10 "soon" would expire, but the batch fails on a missing release.
	_, err := r.Apply(Batch{Now: 10, Ops: []Op{{Kind: Release, LeaseID: "ghost"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	after := r.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state leaked: before=%+v after=%+v", before, after)
	}
	if after.Now != 1 || after.Pools[0].Used != 7 {
		t.Fatalf("time/usage leaked: %+v", after)
	}
	// A successful batch at the same time expires exactly the due lease.
	res := mustApply(t, r, Batch{Now: 10, Ops: []Op{{Kind: Renew, LeaseID: "late", ExpiresAt: 60}}})
	if !reflect.DeepEqual(res.Expired, []string{"soon"}) {
		t.Fatalf("expired=%v", res.Expired)
	}
	if got := r.Snapshot().Pools[0].Used; got != 3 {
		t.Fatalf("used=%d", got)
	}
}

func TestExpiryBoundaryExact(t *testing.T) {
	r := registry(t)
	mustApply(t, r, Batch{Now: 1, Ops: []Op{acq("cpu", "edge", "a", 1, 7)}})
	// ExpiresAt == now means expired.
	res := mustApply(t, r, Batch{Now: 7})
	if !reflect.DeepEqual(res.Expired, []string{"edge"}) {
		t.Fatalf("expired=%v", res.Expired)
	}
	if len(r.Snapshot().Leases) != 0 {
		t.Fatalf("lease survived its expiry boundary")
	}
	// Acquire with ExpiresAt == Batch.Now is invalid (must be strictly greater).
	_, err := r.Apply(Batch{Now: 7, Ops: []Op{acq("cpu", "bad", "a", 1, 7)}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
}

func TestReleaseThenAcquireSameIDSameBatch(t *testing.T) {
	r := registry(t)
	mustApply(t, r, Batch{Now: 1, Ops: []Op{acq("cpu", "x", "a", 2, 9)}})
	res := mustApply(t, r, Batch{Now: 2, Ops: []Op{
		{Kind: Release, LeaseID: "x"},
		acq("gpu", "x", "b", 3, 9),
	}})
	if res.Generation != 2 {
		t.Fatalf("gen=%d", res.Generation)
	}
	s := r.Snapshot()
	if len(s.Leases) != 1 || s.Leases[0].Pool != "gpu" || s.Leases[0].Owner != "b" {
		t.Fatalf("s=%+v", s)
	}
	if s.Pools[0].Name != "cpu" || s.Pools[0].Used != 0 || s.Pools[1].Used != 3 {
		t.Fatalf("pools=%+v", s.Pools)
	}
}

func TestDuplicateAcquireWithinBatch(t *testing.T) {
	r := registry(t)
	before := r.Snapshot()
	_, err := r.Apply(Batch{Now: 1, Ops: []Op{acq("cpu", "dup", "a", 1, 9), acq("cpu", "dup", "b", 1, 9)}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("conflict leaked partial batch")
	}
}

func TestRenewMissingAndExpired(t *testing.T) {
	r := registry(t)
	mustApply(t, r, Batch{Now: 1, Ops: []Op{acq("cpu", "e", "a", 1, 3)}})
	if _, err := r.Apply(Batch{Now: 2, Ops: []Op{{Kind: Renew, LeaseID: "nope", ExpiresAt: 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	// At Now=3 the lease is expired before ops run, so renew misses it.
	if _, err := r.Apply(Batch{Now: 3, Ops: []Op{{Kind: Renew, LeaseID: "e", ExpiresAt: 9}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	// Renew only changes expiry.
	r2 := registry(t)
	mustApply(t, r2, Batch{Now: 1, Ops: []Op{acq("cpu", "k", "alice", 5, 4)}})
	mustApply(t, r2, Batch{Now: 2, Ops: []Op{{Kind: Renew, LeaseID: "k", ExpiresAt: 40}}})
	l := r2.Snapshot().Leases[0]
	if l.ExpiresAt != 40 || l.Weight != 5 || l.Owner != "alice" || l.Pool != "cpu" {
		t.Fatalf("lease=%+v", l)
	}
}

func TestCapacityExactAndMaxLeases(t *testing.T) {
	r := registry(t)
	mustApply(t, r, Batch{Now: 1, Ops: []Op{acq("cpu", "full", "a", 10, 9)}})
	if got := r.Snapshot().Pools[0].Used; got != 10 {
		t.Fatalf("used=%d", got)
	}
	if _, err := r.Apply(Batch{Now: 2, Ops: []Op{acq("cpu", "over", "a", 1, 9)}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	small, err := New(Options{MaxPools: 1, MaxLeases: 1, MaxNameBytes: 8, MaxOwnerBytes: 8}, []PoolConfig{{Name: "p", Capacity: 100}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, small, Batch{Now: 1, Ops: []Op{acq("p", "one", "o", 1, 9)}})
	if _, err := small.Apply(Batch{Now: 2, Ops: []Op{acq("p", "two", "o", 1, 9)}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
}

func TestOverflowGuards(t *testing.T) {
	big, err := New(Options{MaxPools: 1, MaxLeases: 8, MaxNameBytes: 8, MaxOwnerBytes: 8},
		[]PoolConfig{{Name: "p", Capacity: math.MaxUint64}})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, big, Batch{Now: 1, Ops: []Op{acq("p", "a", "o", math.MaxUint64-1, 9)}})
	if _, err := big.Apply(Batch{Now: 2, Ops: []Op{acq("p", "b", "o", 2, 9)}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if got := big.Snapshot().Pools[0].Used; got != math.MaxUint64-1 {
		t.Fatalf("used overflowed: %d", got)
	}
}

func TestTimeMonotonicityAndGeneration(t *testing.T) {
	r := registry(t)
	mustApply(t, r, Batch{Now: 5})
	if _, err := r.Apply(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatalf("err=%v", err)
	}
	if _, err := r.Sweep(4); !errors.Is(err, ErrTime) {
		t.Fatalf("err=%v", err)
	}
	// Equal time is allowed.
	if _, err := r.Apply(Batch{Now: 5}); err != nil {
		t.Fatalf("err=%v", err)
	}
	// Empty successful batch does not bump generation; op batch does.
	if g := r.Snapshot().Generation; g != 0 {
		t.Fatalf("gen=%d", g)
	}
	res := mustApply(t, r, Batch{Now: 6, Ops: []Op{acq("cpu", "g", "a", 1, 9)}})
	if res.Generation != 1 || r.Snapshot().Generation != 1 {
		t.Fatalf("gen=%d", res.Generation)
	}
	// Sweep with nothing due does not bump generation.
	if _, err := r.Sweep(7); err != nil {
		t.Fatal(err)
	}
	if g := r.Snapshot().Generation; g != 1 {
		t.Fatalf("gen=%d", g)
	}
	exp, err := r.Sweep(9)
	if err != nil || !reflect.DeepEqual(exp, []string{"g"}) {
		t.Fatalf("exp=%v err=%v", exp, err)
	}
	if g := r.Snapshot().Generation; g != 2 {
		t.Fatalf("gen=%d", g)
	}
}

func TestStructuralValidationErrors(t *testing.T) {
	r := registry(t)
	cases := []Op{
		{Kind: 0, LeaseID: "x"},
		{Kind: 99, LeaseID: "x"},
		acq("cpu", "", "a", 1, 9),
		acq("cpu", "bad id", "a", 1, 9),
		acq("cpu", "x", "", 1, 9),
		acq("cpu", "x", "a", 0, 9),
		{Kind: Release, LeaseID: "x", Pool: "cpu"},
		{Kind: Release, LeaseID: "x", Weight: 1},
		{Kind: Renew, LeaseID: "x", Owner: "a", ExpiresAt: 9},
		{Kind: Renew, LeaseID: "x"},
	}
	for i, op := range cases {
		if _, err := r.Apply(Batch{Now: 1, Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: err=%v", i, err)
		}
	}
}

func TestUnknownPoolAndSortedSnapshot(t *testing.T) {
	r := registry(t)
	if _, err := r.Apply(Batch{Now: 1, Ops: []Op{acq("nope", "x", "a", 1, 9)}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	mustApply(t, r, Batch{Now: 1, Ops: []Op{
		acq("gpu", "m", "a", 1, 9), acq("cpu", "b", "a", 1, 9), acq("gpu", "a", "a", 1, 9),
	}})
	s := r.Snapshot()
	if s.Pools[0].Name != "cpu" || s.Pools[1].Name != "gpu" {
		t.Fatalf("pools=%+v", s.Pools)
	}
	ids := []string{s.Leases[0].LeaseID, s.Leases[1].LeaseID, s.Leases[2].LeaseID}
	if !reflect.DeepEqual(ids, []string{"a", "b", "m"}) {
		t.Fatalf("ids=%v", ids)
	}
	if s.Pools[1].Leases != 2 {
		t.Fatalf("leases=%+v", s.Pools)
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	r, err := New(Options{MaxPools: 2, MaxLeases: 512, MaxNameBytes: 16, MaxOwnerBytes: 8},
		[]PoolConfig{{Name: "p", Capacity: 256}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("l-%02d", i)
			if _, err := r.Apply(Batch{Now: 1, Ops: []Op{acq("p", id, "o", 1, 50)}}); err != nil {
				t.Error(err)
				return
			}
			if _, err := r.Apply(Batch{Now: 1, Ops: []Op{{Kind: Renew, LeaseID: id, ExpiresAt: 60}}}); err != nil {
				t.Error(err)
				return
			}
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	if _, err := r.Sweep(3); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if len(s.Leases) != 32 || s.Pools[0].Used != 32 || s.Now != 3 {
		t.Fatalf("s=%+v", s)
	}
	for _, l := range s.Leases {
		if l.ExpiresAt != 60 {
			t.Fatalf("lease=%+v", l)
		}
	}
}
