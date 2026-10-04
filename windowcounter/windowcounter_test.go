package windowcounter

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBoundaryExactExpiry(t *testing.T) {
	r := reg(t)
	if _, e := r.Apply(Batch{Now: 5, Deltas: []Delta{d("a", 3)}}); e != nil {
		t.Fatal(e)
	}
	// At == now-Window (5 == 15-10): event must expire.
	v, ok, e := r.Get("a", 15)
	if e != nil || ok || v != 0 {
		t.Fatalf("v=%d ok=%v e=%v", v, ok, e)
	}
	// One tick earlier it must still be live; use a fresh registry.
	r2 := reg(t)
	_, _ = r2.Apply(Batch{Now: 5, Deltas: []Delta{d("a", 3)}})
	v, ok, e = r2.Get("a", 14)
	if e != nil || !ok || v != 3 {
		t.Fatalf("v=%d ok=%v e=%v", v, ok, e)
	}
}

func TestNowLessThanWindow(t *testing.T) {
	r := reg(t)
	if _, e := r.Apply(Batch{Now: 0, Deltas: []Delta{d("a", 1)}}); e != nil {
		t.Fatal(e)
	}
	// now < Window: cutoff negative, nothing can expire.
	n, e := r.Sweep(9)
	if e != nil || n != 0 {
		t.Fatalf("n=%d e=%v", n, e)
	}
	v, ok, _ := r.Get("a", 9)
	if !ok || v != 1 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
}

func TestExpiryRollbackOnFailure(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("old", 2)}})
	_, _ = r.Apply(Batch{Now: 5, Deltas: []Delta{d("a", 1)}})
	before := r.Snapshot()
	// Batch at now=20 would expire "old", but underflows: expiry must roll back.
	_, e := r.Apply(Batch{Now: 20, Deltas: []Delta{d("a", -5)}})
	if !errors.Is(e, ErrUnderflow) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("state leaked: %+v", r.Snapshot())
	}
	// Capacity failure must also roll back expiry, time and generation.
	r2, _ := New(Options{Window: 5, MaxKeys: 1, MaxEvents: 8, MaxNameBytes: 8})
	_, _ = r2.Apply(Batch{Now: 1, Deltas: []Delta{d("old", 1)}})
	before = r2.Snapshot()
	_, e = r2.Apply(Batch{Now: 10, Deltas: []Delta{d("n1", 1), d("n2", 1)}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r2.Snapshot()) {
		t.Fatalf("state leaked: %+v", r2.Snapshot())
	}
}

func TestSameKeyOrderWithinBatch(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 5)}})
	// -5 then +2: legal (sum hits 0, never negative).
	x, e := r.Apply(Batch{Now: 2, Deltas: []Delta{d("a", -5), d("a", 2)}})
	if e != nil || x.Counts[0].Value != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// +2 then -5 would be fine, but -6 first must underflow even if later +10.
	before := r.Snapshot()
	_, e = r.Apply(Batch{Now: 3, Deltas: []Delta{d("a", -6), d("a", 10)}})
	if !errors.Is(e, ErrUnderflow) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("e=%v s=%+v", e, r.Snapshot())
	}
}

func TestOverflowAndUnderflowEdges(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", math.MaxInt64)}})
	if _, e := r.Apply(Batch{Now: 2, Deltas: []Delta{d("a", 1)}}); !errors.Is(e, ErrOverflow) {
		t.Fatalf("e=%v", e)
	}
	// MinInt64 delta against a small sum must underflow, not wrap.
	before := r.Snapshot()
	if _, e := r.Apply(Batch{Now: 3, Deltas: []Delta{d("a", math.MinInt64)}}); !errors.Is(e, ErrUnderflow) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("state leaked")
	}
	// Zero amount and bad keys rejected before any state check.
	if _, e := r.Apply(Batch{Now: 4, Deltas: []Delta{d("a", 0)}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	if _, e := r.Apply(Batch{Now: 4, Deltas: []Delta{d("bad key", 1)}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
}

func TestFinalCapacityAllowsReplacement(t *testing.T) {
	r, _ := New(Options{Window: 5, MaxKeys: 2, MaxEvents: 2, MaxNameBytes: 8})
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 1), d("b", 1)}})
	// At now=10 both old events expire, freeing capacity for two new keys.
	x, e := r.Apply(Batch{Now: 10, Deltas: []Delta{d("c", 1), d("d", 1)}})
	if e != nil || x.ExpiredEvents != 2 || len(x.Counts) != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Event capacity exceeded only in final state.
	before := r.Snapshot()
	if _, e = r.Apply(Batch{Now: 11, Deltas: []Delta{d("c", 1)}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("state leaked")
	}
}

func TestGetSweepAdvanceTimeAndGeneration(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 2)}})
	g := r.Snapshot().Generation
	// Get with no expiry: time advances, generation unchanged.
	if _, _, e := r.Get("a", 5); e != nil {
		t.Fatal(e)
	}
	s := r.Snapshot()
	if s.Now != 5 || s.Generation != g {
		t.Fatalf("s=%+v", s)
	}
	// Sweep past the window: one expiry, generation +1.
	n, e := r.Sweep(20)
	if e != nil || n != 1 {
		t.Fatalf("n=%d e=%v", n, e)
	}
	if r.Snapshot().Generation != g+1 {
		t.Fatalf("gen=%d", r.Snapshot().Generation)
	}
	// Empty sweep: no generation bump.
	if n, e = r.Sweep(30); e != nil || n != 0 || r.Snapshot().Generation != g+1 {
		t.Fatalf("n=%d e=%v gen=%d", n, e, r.Snapshot().Generation)
	}
	// Time cannot move backwards on Get/Sweep.
	if _, _, e = r.Get("a", 29); !errors.Is(e, ErrTime) {
		t.Fatalf("e=%v", e)
	}
	if _, e = r.Sweep(29); !errors.Is(e, ErrTime) {
		t.Fatalf("e=%v", e)
	}
	// Invalid key on Get.
	if _, _, e = r.Get("bad!", 30); !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("e=%v", e)
	}
	// Unknown key: ok=false.
	if v, ok, e := r.Get("nope", 30); e != nil || ok || v != 0 {
		t.Fatalf("v=%d ok=%v e=%v", v, ok, e)
	}
}

func TestEmptyBatchTimeOnly(t *testing.T) {
	r := reg(t)
	_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d("a", 1)}})
	g := r.Snapshot().Generation
	x, e := r.Apply(Batch{Now: 5})
	if e != nil || x.Generation != g || len(x.Counts) != 0 || x.ExpiredEvents != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if r.Snapshot().Now != 5 {
		t.Fatal("time not advanced")
	}
	// Empty batch that expires something does bump generation.
	x, e = r.Apply(Batch{Now: 20})
	if e != nil || x.ExpiredEvents != 1 || x.Generation != g+1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestCountsAndSnapshotSorted(t *testing.T) {
	r := reg(t)
	x, e := r.Apply(Batch{Now: 1, Deltas: []Delta{d("z", 1), d("m", 2), d("a", 3)}})
	if e != nil {
		t.Fatal(e)
	}
	want := []Count{{"a", 3}, {"m", 2}, {"z", 1}}
	if !reflect.DeepEqual(x.Counts, want) {
		t.Fatalf("counts=%+v", x.Counts)
	}
	s := r.Snapshot()
	keys := []string{s.Keys[0].Key, s.Keys[1].Key, s.Keys[2].Key}
	if !reflect.DeepEqual(keys, []string{"a", "m", "z"}) {
		t.Fatalf("keys=%v", keys)
	}
	if s.Events != 3 {
		t.Fatalf("events=%d", s.Events)
	}
	// Ownership isolation: mutating returned slices must not affect the registry.
	x.Counts[0].Key = "hacked"
	s.Keys[0].Key = "hacked"
	s2 := r.Snapshot()
	if s2.Keys[0].Key != "a" {
		t.Fatalf("snapshot aliased: %+v", s2.Keys[0])
	}
}

func TestConcurrentMixed(t *testing.T) {
	r, _ := New(Options{Window: 100, MaxKeys: 512, MaxEvents: 4096, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k-%02d", i)
			for n := 0; n < 50; n++ {
				_, _ = r.Apply(Batch{Now: 1, Deltas: []Delta{d(k, 1)}})
				_, _, _ = r.Get(k, 1)
				_, _ = r.Sweep(1)
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if s.Now != 1 || s.Events != 32*50 || len(s.Keys) != 32 {
		t.Fatalf("s.Now=%d events=%d keys=%d", s.Now, s.Events, len(s.Keys))
	}
	for _, ks := range s.Keys {
		if ks.Value != 50 || ks.Events != 50 {
			t.Fatalf("ks=%+v", ks)
		}
	}
}
