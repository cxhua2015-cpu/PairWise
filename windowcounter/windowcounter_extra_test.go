package windowcounter

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWindowBoundaryExact(t *testing.T) {
	r := reg(t)
	if _, e := r.Apply(Batch{Now: 5, Deltas: []Delta{d("a", 1), d("a", 1)}}); e != nil {
		t.Fatal(e)
	}
	// now-Window == 5: events at 5 are NOT retained (need at > now-Window).
	x, e := r.Apply(Batch{Now: 15, Deltas: []Delta{d("b", 1)}})
	if e != nil || x.ExpiredEvents != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if v, ok, _ := r.Get("a", 15); ok || v != 0 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
	// now-Window == 4: events at 5 retained.
	r2 := reg(t)
	_, _ = r2.Apply(Batch{Now: 5, Deltas: []Delta{d("a", 2)}})
	if v, ok, e := r2.Get("a", 14); e != nil || !ok || v != 2 {
		t.Fatalf("v=%d ok=%v e=%v", v, ok, e)
	}
}

func TestNowLessThanWindow(t *testing.T) {
	r := reg(t) // Window=10
	if _, e := r.Apply(Batch{Now: 3, Deltas: []Delta{d("a", 4)}}); e != nil {
		t.Fatal(e)
	}
	// now < Window: nothing can expire.
	x, e := r.Apply(Batch{Now: 9, Deltas: []Delta{d("a", 1)}})
	if e != nil || x.ExpiredEvents != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if v, ok, _ := r.Get("a", 9); !ok || v != 5 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
}

func TestExpiryRollbackOnFailure(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 2)}})
	before := r.Snapshot()
	// Batch would expire the old events, then underflow: full rollback.
	_, e := r.Apply(Batch{Now: 20, Deltas: []Delta{d("b", 1), d("b", -5)}})
	if !errors.Is(e, ErrUnderflow) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("state leaked: %+v", r.Snapshot())
	}
	// Time and generation must not leak either.
	if _, e := r.Apply(Batch{Now: 19}); e != nil {
		t.Fatalf("time leaked: %v", e)
	}
	if g := r.Snapshot().Generation; g != before.Generation+1 {
		t.Fatalf("generation=%d want %d", g, before.Generation+1)
	}
}

func TestSameKeyOrderWithinBatch(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 3)}})
	// Order matters: -3 then +2 succeeds; +2 then -4 would underflow.
	x, e := r.Apply(Batch{Now: 2, Deltas: []Delta{d("a", -3), d("a", 2)}})
	if e != nil || x.Counts[0].Value != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	before := r.Snapshot()
	_, e = r.Apply(Batch{Now: 3, Deltas: []Delta{d("a", 1), d("a", -4)}})
	if !errors.Is(e, ErrUnderflow) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
}

func TestOverflowExactBoundary(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", math.MaxInt64)}})
	before := r.Snapshot()
	if _, e := r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 1)}}); !errors.Is(e, ErrOverflow) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("overflow leaked state")
	}
	// MaxInt64-1 plus 1 is fine.
	r2 := reg(t)
	_, _ = r2.Apply(Batch{Now: 1, Deltas: []Delta{d("a", math.MaxInt64-1)}})
	x, e := r2.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 1)}})
	if e != nil || x.Counts[0].Value != math.MaxInt64 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestFinalCapacityAllowsExpiryReplacement(t *testing.T) {
	r, e := New(Options{Window: 5, MaxKeys: 1, MaxEvents: 2, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 1), d("a", 1)}})
	// Old key expires during the batch, freeing capacity for the new key.
	x, e := r.Apply(Batch{Now: 7, Deltas: []Delta{d("b", 1), d("b", 1)}})
	if e != nil || x.ExpiredEvents != 2 || len(x.Counts) != 1 || x.Counts[0].Key != "b" {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Event capacity failure rolls back.
	before := r.Snapshot()
	if _, e := r.Apply(Batch{Now: 7, Deltas: []Delta{d("b", 1)}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("capacity failure leaked state")
	}
}

func TestGetSweepAdvanceTimeAndGeneration(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 2, Deltas: []Delta{d("a", 1)}})
	g0 := r.Snapshot().Generation
	// Get with no expiry: time advances, generation unchanged.
	if _, _, e := r.Get("a", 5); e != nil {
		t.Fatal(e)
	}
	if s := r.Snapshot(); s.Now != 5 || s.Generation != g0 {
		t.Fatalf("s=%+v", s)
	}
	// Get past the window: expires, generation +1.
	if v, ok, _ := r.Get("a", 13); ok || v != 0 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
	if s := r.Snapshot(); s.Generation != g0+1 || s.Events != 0 {
		t.Fatalf("s=%+v", s)
	}
	// Sweep with nothing to expire: no generation bump.
	if n, e := r.Sweep(30); e != nil || n != 0 {
		t.Fatalf("n=%d e=%v", n, e)
	}
	if s := r.Snapshot(); s.Generation != g0+1 || s.Now != 30 {
		t.Fatalf("s=%+v", s)
	}
	// Time cannot move backwards via Get or Sweep.
	if _, _, e := r.Get("a", 29); !errors.Is(e, ErrTime) {
		t.Fatalf("e=%v", e)
	}
	if _, e := r.Sweep(0); !errors.Is(e, ErrTime) {
		t.Fatalf("e=%v", e)
	}
	// Invalid key rejected before touching state.
	if _, _, e := r.Get("bad key!", 40); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
}

func TestEmptyBatchTimeOnly(t *testing.T) {
	r := reg(t)
	x, e := r.Apply(Batch{Now: 7})
	if e != nil || x.ExpiredEvents != 0 || len(x.Counts) != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	s := r.Snapshot()
	if s.Now != 7 || s.Generation != 0 {
		t.Fatalf("s=%+v", s)
	}
}

func TestSortedCountsAndSnapshotIsolation(t *testing.T) {
	r := reg(t)
	x, e := r.Apply(Batch{Now: 1, Deltas: []Delta{d("z", 1), d("a", 2), d("m", 3)}})
	if e != nil {
		t.Fatal(e)
	}
	want := []Count{{"a", 2}, {"m", 3}, {"z", 1}}
	if !reflect.DeepEqual(x.Counts, want) {
		t.Fatalf("counts=%+v", x.Counts)
	}
	s1 := r.Snapshot()
	if len(s1.Keys) != 3 || s1.Keys[0].Key != "a" || s1.Keys[2].Key != "z" {
		t.Fatalf("s1=%+v", s1)
	}
	// Mutating returned slices must not affect the registry.
	s1.Keys[0].Key = "hacked"
	s1.Keys[0].Value = -99
	x.Counts[0].Value = -99
	s2 := r.Snapshot()
	if s2.Keys[0].Key != "a" || s2.Keys[0].Value != 2 {
		t.Fatalf("snapshot not isolated: %+v", s2)
	}
}

func TestConcurrentMixed(t *testing.T) {
	r, e := New(Options{Window: 50, MaxKeys: 256, MaxEvents: 4096, MaxNameBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var clock atomic.Int64
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				n := clock.Add(1)
				_, _ = r.Apply(Batch{Now: n, Deltas: []Delta{d(k, 1)}})
				_, _, _ = r.Get(k, n)
				_, _ = r.Sweep(n)
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Keys) > 32 || s.Now < 1 {
		t.Fatalf("s=%+v", s)
	}
	var sum int64
	events := 0
	for _, ks := range s.Keys {
		if ks.Value <= 0 || ks.Events <= 0 {
			t.Fatalf("key %s: %+v", ks.Key, ks)
		}
		sum += ks.Value
		events += ks.Events
	}
	if events != s.Events || sum != int64(events) {
		t.Fatalf("inconsistent snapshot: %+v", s)
	}
}

func TestKeyValidation(t *testing.T) {
	r, _ := New(Options{Window: 10, MaxKeys: 8, MaxEvents: 16, MaxNameBytes: 6})
	for _, bad := range []string{"", "abcdefg", "a b", "a!", "é"} {
		if _, e := r.Apply(Batch{Now: 1, Deltas: []Delta{d(bad, 1)}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("key %q: e=%v", bad, e)
		}
	}
	for _, good := range []string{"a", "A1._/-", "abcd"} {
		if _, e := r.Apply(Batch{Now: 1, Deltas: []Delta{d(good, 1)}}); e != nil {
			t.Fatalf("key %q: %v", good, e)
		}
	}
	if _, e := r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 0)}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("zero amount: %v", e)
	}
}
