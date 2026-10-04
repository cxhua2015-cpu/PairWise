package rangelock

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchRollbackOnCapacity(t *testing.T) {
	m := manager(t, Options{MaxLocks: 2, MaxOwners: 8, MaxMetadataBytes: 64, MaxNameBytes: 16})
	_, _, _ = m.AcquireBatch(0, []Request{{"a", "o1", "r", 0, 10, Read, 100, nil}})
	before := m.Snapshot()
	// Batch itself fits (1 existing + 1 new = 2), but second request overflows.
	_, _, err := m.AcquireBatch(0, []Request{
		{"b", "o2", "r", 20, 30, Read, 10, nil},
		{"c", "o3", "r", 40, 50, Read, 10, nil},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
	// Token not consumed: next successful acquire gets before.NextToken.
	leases, _, err := m.AcquireBatch(0, []Request{{"b", "o2", "r", 20, 30, Read, 10, nil}})
	if err != nil || leases[0].Token != before.NextToken {
		t.Fatalf("leases=%v err=%v", leases, err)
	}
}

func TestBatchRollbackOnOwnerCapacity(t *testing.T) {
	m := manager(t, Options{MaxLocks: 8, MaxOwners: 1, MaxMetadataBytes: 64, MaxNameBytes: 16})
	_, _, _ = m.AcquireBatch(0, []Request{{"a", "o1", "r", 0, 10, Read, 100, nil}})
	before := m.Snapshot()
	if _, _, err := m.AcquireBatch(0, []Request{{"b", "o2", "r", 20, 30, Read, 10, nil}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("owner capacity failure mutated state")
	}
}

func TestBatchRollbackOnMetadataCapacity(t *testing.T) {
	m := manager(t, Options{MaxLocks: 8, MaxOwners: 8, MaxMetadataBytes: 4, MaxNameBytes: 16})
	_, _, _ = m.AcquireBatch(0, []Request{{"a", "o1", "r", 0, 10, Read, 100, []byte("ab")}})
	before := m.Snapshot()
	_, _, err := m.AcquireBatch(0, []Request{{"b", "o2", "r", 20, 30, Read, 10, []byte("cde")}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("metadata capacity failure mutated state")
	}
}

func TestFailedBatchDoesNotPruneExpiry(t *testing.T) {
	m := manager(t, opts())
	_, _, _ = m.AcquireBatch(0, []Request{{"x", "o", "r", 0, 5, Write, 2, nil}})
	// Failing batch at now=2 must not prune the expired lock.
	_, _, err := m.AcquireBatch(2, []Request{{"y", "o", "r", 10, 20, Read, 5, nil}, {"y", "o", "r", 30, 40, Read, 5, nil}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	s := m.Snapshot()
	if s.Locks != 1 || s.Leases[0].ID != "x" {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestExpiryTakeoverSameID(t *testing.T) {
	m := manager(t, opts())
	first, _, _ := m.AcquireBatch(0, []Request{{"id", "old", "r", 0, 5, Write, 3, nil}})
	// Exactly at ExpiresAt the lock is expired and can be re-acquired.
	next, _, err := m.AcquireBatch(3, []Request{{"id", "new", "r", 0, 5, Write, 2, nil}})
	if err != nil || next[0].Token != first[0].Token+1 {
		t.Fatalf("next=%v err=%v", next, err)
	}
	// Old token is now stale for renew.
	if err := m.Renew("id", first[0].Token, 3, 5); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("renew=%v", err)
	}
}

func TestInt64Extremes(t *testing.T) {
	m := manager(t, opts())
	_, _, err := m.AcquireBatch(0, []Request{{"a", "o", "r", math.MinInt64, math.MaxInt64, Write, 10, nil}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	// Adjacent at extremes does not conflict.
	_, _, err = m.AcquireBatch(0, []Request{{"b", "o", "r", math.MinInt64, math.MinInt64 + 1, Write, 10, nil}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	q, err := m.Query("r", math.MinInt64, math.MaxInt64, 0)
	if err != nil || len(q) != 1 {
		t.Fatalf("query=%v err=%v", q, err)
	}
	// now+TTL overflow rejected.
	if _, _, err = m.AcquireBatch(math.MaxInt64-1, []Request{{"c", "o", "r", 0, 1, Read, 2, nil}}); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("overflow=%v", err)
	}
	if err := m.Renew("a", 1, math.MaxInt64-1, 2); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("renew overflow=%v", err)
	}
}

func TestTokenFencingMonotonic(t *testing.T) {
	// Sequential batches get increasing tokens and generation advances by 1.
	m2 := manager(t, opts())
	g0 := m2.Snapshot().Generation
	var prev uint64
	for i := 0; i < 4; i++ {
		leases, gen, err := m2.AcquireBatch(0, []Request{{ID: string(rune('a' + i)), Owner: "o", Resource: "r", Start: int64(i * 10), End: int64(i*10 + 5), Mode: Read, TTL: 100}})
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && leases[0].Token != prev+1 {
			t.Fatalf("token %d after %d", leases[0].Token, prev)
		}
		prev = leases[0].Token
		if gen != g0+uint64(i+1) {
			t.Fatalf("generation=%d", gen)
		}
	}
	// Empty batch returns current generation without advancing.
	_, gen, err := m2.AcquireBatch(0, nil)
	if err != nil || gen != g0+4 || m2.Snapshot().Generation != g0+4 {
		t.Fatalf("empty batch gen=%d err=%v", gen, err)
	}
}

func TestMetadataIsolationBetweenReturns(t *testing.T) {
	m := manager(t, opts())
	in := []byte("abc")
	leases, _, _ := m.AcquireBatch(0, []Request{{"a", "o", "r", 0, 5, Read, 10, in}})
	in[0] = 'X'
	if string(leases[0].Metadata) != "abc" {
		t.Fatalf("input alias: %q", leases[0].Metadata)
	}
	q1, _ := m.Query("r", 0, 5, 0)
	q2, _ := m.Query("r", 0, 5, 0)
	q1[0].Metadata[0] = 'Z'
	if !bytes.Equal(q2[0].Metadata, []byte("abc")) {
		t.Fatalf("returns alias: %q", q2[0].Metadata)
	}
	s := m.Snapshot()
	if !bytes.Equal(s.Leases[0].Metadata, []byte("abc")) {
		t.Fatalf("state mutated: %q", s.Leases[0].Metadata)
	}
}

func TestRenewSuccessAndGeneration(t *testing.T) {
	m := manager(t, opts())
	leases, _, _ := m.AcquireBatch(1, []Request{{"a", "o", "r", 0, 1, Read, 4, nil}})
	g := m.Snapshot().Generation
	if err := m.Renew("a", leases[0].Token, 4, 6); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if s.Generation != g+1 || s.Leases[0].ExpiresAt != 10 {
		t.Fatalf("snapshot=%+v", s)
	}
	// Renew with unknown ID.
	if err := m.Renew("missing", 1, 0, 1); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("err=%v", err)
	}
}

func TestSweepLimitAndOrdering(t *testing.T) {
	m := manager(t, opts())
	_, _, _ = m.AcquireBatch(0, []Request{
		{"c", "o", "r", 0, 1, Read, 2, nil},
		{"a", "o", "r", 2, 3, Read, 2, nil},
		{"b", "o", "r", 4, 5, Read, 2, nil},
	})
	g := m.Snapshot().Generation
	ids, err := m.Sweep(2, 2)
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if m.Snapshot().Generation != g+1 {
		t.Fatal("generation not advanced")
	}
	// Sweep with nothing expired is a no-op for generation.
	g = m.Snapshot().Generation
	ids, _ = m.Sweep(0, 0)
	if len(ids) != 0 || m.Snapshot().Generation != g {
		t.Fatalf("ids=%v", ids)
	}
	if _, err := m.Sweep(-1, 0); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("err=%v", err)
	}
	if _, err := m.Sweep(0, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Unlimited sweep removes the rest.
	ids, _ = m.Sweep(2, 0)
	if !reflect.DeepEqual(ids, []string{"c"}) {
		t.Fatalf("ids=%v", ids)
	}
}

func TestReleaseUnsweptExpired(t *testing.T) {
	m := manager(t, opts())
	leases, _, _ := m.AcquireBatch(0, []Request{{"a", "o", "r", 0, 1, Read, 2, nil}})
	if err := m.Release("a", leases[0].Token); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Locks != 0 {
		t.Fatal("not released")
	}
	if err := m.Release("a", leases[0].Token); !errors.Is(err, ErrStaleToken) {
		t.Fatalf("err=%v", err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	m := manager(t, Options{MaxLocks: 256, MaxOwners: 256, MaxMetadataBytes: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('A' + i))
			leases, _, err := m.AcquireBatch(0, []Request{{id, id, "r", int64(i * 10), int64(i*10 + 5), Write, 50, []byte("m")}})
			if err != nil {
				t.Errorf("acquire=%v", err)
				return
			}
			_ = m.Renew(id, leases[0].Token, 1, 50)
			_, _ = m.Query("r", 0, 200, 1)
			_ = m.Snapshot()
			_, _ = m.Sweep(100, 0)
			_ = m.Release(id, leases[0].Token)
		}()
	}
	wg.Wait()
	s := m.Snapshot()
	if s.Locks != 0 {
		t.Fatalf("locks=%d", s.Locks)
	}
}
