package rangelock

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchRollbackRestoresExpiredLocks(t *testing.T) {
	m := manager(t, Options{MaxLocks: 1, MaxOwners: 2, MaxMetadataBytes: 16, MaxNameBytes: 8})
	_, _, _ = m.AcquireBatch(0, []Request{{"old", "o1", "r", 0, 5, Write, 3, nil}})
	before := m.Snapshot()
	// Batch prunes "old" in the candidate, then exceeds MaxLocks -> rollback.
	_, _, err := m.AcquireBatch(3, []Request{
		{"n1", "o1", "r", 0, 5, Write, 5, nil},
		{"n2", "o2", "r", 10, 20, Read, 5, nil},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	after := m.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback mismatch: %+v vs %+v", before, after)
	}
	if after.NextToken != 2 {
		t.Fatalf("token consumed: %d", after.NextToken)
	}
}

func TestCapacityOwnerAndMetadataChecks(t *testing.T) {
	m := manager(t, Options{MaxLocks: 4, MaxOwners: 1, MaxMetadataBytes: 64, MaxNameBytes: 8})
	_, _, _ = m.AcquireBatch(0, []Request{{"a", "o1", "r", 0, 1, Read, 100, nil}})
	before := m.Snapshot()
	if _, _, err := m.AcquireBatch(0, []Request{{"b", "o2", "r", 10, 20, Read, 100, nil}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("owners=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("owner capacity mutated state")
	}
	m2 := manager(t, Options{MaxLocks: 4, MaxOwners: 4, MaxMetadataBytes: 3, MaxNameBytes: 8})
	if _, _, err := m2.AcquireBatch(0, []Request{
		{"a", "o1", "r", 0, 1, Read, 100, []byte("ab")},
		{"b", "o2", "r", 2, 3, Read, 100, []byte("cd")},
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("metadata=%v", err)
	}
	if s := m2.Snapshot(); s.Locks != 0 || s.Generation != 0 {
		t.Fatalf("metadata capacity mutated: %+v", s)
	}
}

func TestExtremeInt64Boundaries(t *testing.T) {
	m := manager(t, opts())
	leases, _, err := m.AcquireBatch(math.MaxInt64-10, []Request{
		{"lo", "o1", "r", math.MinInt64, math.MinInt64 + 5, Write, 10, nil},
		{"hi", "o2", "r", math.MaxInt64 - 5, math.MaxInt64, Write, 10, nil},
	})
	if err != nil || leases[0].ExpiresAt != math.MaxInt64 {
		t.Fatalf("leases=%v err=%v", leases, err)
	}
	// Adjacent at extremes do not conflict.
	if _, _, err := m.AcquireBatch(math.MaxInt64-10, []Request{
		{"lo2", "o1", "r", math.MinInt64 + 5, math.MinInt64 + 9, Write, 5, nil},
	}); err != nil {
		t.Fatalf("adjacent=%v", err)
	}
	// Query window covering extremes.
	q, err := m.Query("r", math.MinInt64, math.MaxInt64, math.MaxInt64-10)
	if err != nil || len(q) != 3 {
		t.Fatalf("query=%v err=%v", len(q), err)
	}
	// Overflowing TTL rejected.
	if _, _, err := m.AcquireBatch(math.MaxInt64, []Request{{"x", "o", "r", 0, 1, Read, 1, nil}}); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow=%v", err)
	}
	// Renew overflow rejected, exact-boundary renew allowed.
	if err := m.Renew("lo", leases[0].Token, math.MaxInt64-1, 2); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("renew overflow=%v", err)
	}
	if err := m.Renew("lo", leases[0].Token, math.MaxInt64-1, 1); err != nil {
		t.Fatalf("renew boundary=%v", err)
	}
	if got := m.Snapshot().Leases; got[0].ExpiresAt != math.MaxInt64 {
		t.Fatalf("expires=%d", got[0].ExpiresAt)
	}
}

func TestFencingTokenMonotonicAcrossBatches(t *testing.T) {
	m := manager(t, opts())
	var last uint64
	for i := 0; i < 5; i++ {
		leases, gen, err := m.AcquireBatch(0, []Request{{string(rune('a' + i)), "o", "r", int64(i * 10), int64(i*10 + 5), Read, 100, nil}})
		if err != nil {
			t.Fatal(err)
		}
		if leases[0].Token != last+1 || gen != uint64(i+1) {
			t.Fatalf("token=%d gen=%d", leases[0].Token, gen)
		}
		last = leases[0].Token
	}
	// Failed batch does not consume tokens or generation.
	if _, _, err := m.AcquireBatch(0, []Request{{"zz", "o", "r", 0, 100, Write, 1, nil}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	leases, gen, _ := m.AcquireBatch(0, []Request{{"zz", "o", "r", 1000, 1001, Read, 1, nil}})
	if leases[0].Token != last+1 || gen != 6 {
		t.Fatalf("token=%d gen=%d", leases[0].Token, gen)
	}
	// Stale token cannot renew after takeover.
	_ = m.Release("zz", leases[0].Token)
	take, _, _ := m.AcquireBatch(0, []Request{{"zz", "o", "r", 2000, 2001, Read, 10, nil}})
	if err := m.Renew("zz", leases[0].Token, 1, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("stale renew=%v", err)
	}
	if err := m.Renew("zz", take[0].Token, 1, 5); err != nil {
		t.Fatalf("fresh renew=%v", err)
	}
}

func TestExpiryTakeoverAndSweepSemantics(t *testing.T) {
	m := manager(t, opts())
	old, _, _ := m.AcquireBatch(0, []Request{{"k", "o1", "r", 0, 10, Write, 5, nil}})
	// Not yet expired at now=4.
	if _, _, err := m.AcquireBatch(4, []Request{{"k2", "o2", "r", 1, 2, Read, 5, nil}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("early=%v", err)
	}
	// Expired at now=5 (now >= ExpiresAt): takeover succeeds.
	take, _, err := m.AcquireBatch(5, []Request{{"k2", "o2", "r", 1, 2, Write, 5, nil}})
	if err != nil {
		t.Fatalf("takeover=%v", err)
	}
	// Old token is stale now; release with old token fails.
	if err := m.Release("k", old[0].Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("old release=%v", err)
	}
	// Unswept expired lock can still be released by matching token.
	m2 := manager(t, opts())
	l, _, _ := m2.AcquireBatch(0, []Request{{"e", "o", "r", 0, 1, Read, 2, nil}})
	if err := m2.Release("e", l[0].Token); err != nil {
		t.Fatalf("release expired=%v", err)
	}
	// Sweep ordering, limit, and generation.
	m3 := manager(t, opts())
	_, _, _ = m3.AcquireBatch(0, []Request{
		{"b", "o", "r", 0, 1, Read, 2, nil},
		{"a", "o", "r", 2, 3, Read, 2, nil},
		{"c", "o", "r", 4, 5, Read, 100, nil},
	})
	ids, err := m3.Sweep(2, 1)
	if err != nil || !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatalf("sweep1=%v %v", ids, err)
	}
	ids, _ = m3.Sweep(2, 0)
	if !reflect.DeepEqual(ids, []string{"b"}) {
		t.Fatalf("sweep2=%v", ids)
	}
	gen := m3.Snapshot().Generation
	ids, _ = m3.Sweep(2, 0)
	if ids != nil || m3.Snapshot().Generation != gen {
		t.Fatalf("empty sweep changed generation: %v", ids)
	}
	if _, err := m3.Sweep(-1, 0); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("sweep time=%v", err)
	}
	if _, err := m3.Sweep(0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("sweep limit=%v", err)
	}
	_ = take
}

func TestMetadataIsolationAcrossCalls(t *testing.T) {
	m := manager(t, opts())
	in := []byte("abc")
	leases, _, _ := m.AcquireBatch(0, []Request{{"m", "o", "r", 0, 1, Read, 100, in}})
	in[0] = 'X'
	q, _ := m.Query("r", 0, 1, 0)
	s := m.Snapshot()
	if !bytes.Equal(leases[0].Metadata, []byte("abc")) ||
		!bytes.Equal(q[0].Metadata, []byte("abc")) ||
		!bytes.Equal(s.Leases[0].Metadata, []byte("abc")) {
		t.Fatal("input alias")
	}
	q[0].Metadata[0] = 'Y'
	s.Leases[0].Metadata[0] = 'Z'
	again, _ := m.Query("r", 0, 1, 0)
	if !bytes.Equal(again[0].Metadata, []byte("abc")) {
		t.Fatal("return alias")
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	m := manager(t, Options{MaxLocks: 256, MaxOwners: 256, MaxMetadataBytes: 4096, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a'+i)) + "-w"
			for j := 0; j < 20; j++ {
				now := int64(j)
				leases, _, err := m.AcquireBatch(now, []Request{{id, id, "res", int64(i * 100), int64(i*100 + 50), Write, 3, []byte("d")}})
				if err == nil {
					_ = m.renewHelper(leases[0], now)
					_ = m.Release(id, leases[0].Token)
				}
				_, _ = m.Query("res", 0, 1600, now)
				_, _ = m.Sweep(now+10, 0)
				_ = m.Snapshot()
			}
		}()
	}
	wg.Wait()
	if s := m.Snapshot(); s.Locks > 16 {
		t.Fatalf("leaked locks: %+v", s)
	}
}

func (m *Manager) renewHelper(l Lease, now int64) error {
	return m.Renew(l.ID, l.Token, now, 5)
}

func TestEmptyBatchAndDuplicateIDs(t *testing.T) {
	m := manager(t, opts())
	if _, _, err := m.AcquireBatch(-1, nil); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("neg now=%v", err)
	}
	leases, gen, err := m.AcquireBatch(0, nil)
	if err != nil || leases != nil || gen != 0 {
		t.Fatalf("empty=%v %d %v", leases, gen, err)
	}
	_, gen2, _ := m.AcquireBatch(0, []Request{})
	if gen2 != 0 {
		t.Fatalf("empty batch advanced generation: %d", gen2)
	}
	if _, _, err := m.AcquireBatch(0, []Request{
		{"d", "o", "r", 0, 1, Read, 1, nil},
		{"d", "o", "r", 2, 3, Read, 1, nil},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dup=%v", err)
	}
}
