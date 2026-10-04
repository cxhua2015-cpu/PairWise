package tokenbucket

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchRollbackPartialRefundOverflow(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 5, 0), acquire("b", 5, 0)}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	// Refund a (ok), then refund b beyond capacity -> whole batch rolls back.
	_, err := r.ApplyBatch([]Change{refund("a", 3, 0), refund("b", 16, 0)})
	if !errors.Is(err, ErrOverflow) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("rollback failed")
	}
}

func TestCrossBucketIsolation(t *testing.T) {
	r := newRegistry(t)
	// Drain a only; b must stay full and keep its own clock.
	if _, err := r.ApplyBatch([]Change{acquire("a", 10, 3)}); err != nil {
		t.Fatal(err)
	}
	s, err := r.Inspect("b", 3)
	if err != nil || s.Tokens != 20 || s.LastObserved != 3 {
		t.Fatalf("b=%+v %v", s, err)
	}
	// Inspect is side-effect free: stored LastObserved stays 0.
	if got := r.Snapshot().Buckets[1].LastObserved; got != 0 {
		t.Fatalf("stored LastObserved=%d", got)
	}
	// Refill on a is based on a's own LastRefill.
	s, err = r.Inspect("a", 10)
	if err != nil || s.Tokens != 6 || s.LastRefill != 10 {
		t.Fatalf("a=%+v %v", s, err)
	}
}

func TestRefillBoundaryExactAndCap(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 9, 0)}); err != nil {
		t.Fatal(err)
	}
	// One full period: +3.
	s, _ := r.Inspect("a", 5)
	if s.Tokens != 4 || s.LastRefill != 5 || s.NextRefill != 10 {
		t.Fatalf("s=%+v", s)
	}
	// Many periods: capped at capacity, boundary advances to latest complete period.
	s, _ = r.Inspect("a", 24)
	if s.Tokens != 10 || s.LastRefill != 20 || s.NextRefill != 0 {
		t.Fatalf("s=%+v", s)
	}
	// Partial period does not count.
	s, _ = r.Inspect("a", 26)
	if s.Tokens != 10 || s.LastRefill != 25 {
		t.Fatalf("s=%+v", s)
	}
}

func TestRefundToExactCapacity(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 4, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyBatch([]Change{refund("a", 4, 0)}); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Inspect("a", 0)
	if s.Tokens != 10 || s.NextRefill != 0 {
		t.Fatalf("s=%+v", s)
	}
}

func TestExtremeSweepAndRefundOverflowArithmetic(t *testing.T) {
	r, err := New(options(), []BucketSpec{{Name: "big", Capacity: math.MaxInt64, RefillTokens: math.MaxInt64, RefillEvery: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyBatch([]Change{acquire("big", 1, 0)}); err != nil {
		t.Fatal(err)
	}
	// Refund of MaxInt64-1 would exceed capacity: must not overflow arithmetic.
	if _, err := r.ApplyBatch([]Change{refund("big", math.MaxInt64-1, 0)}); !errors.Is(err, ErrOverflow) {
		t.Fatalf("err=%v", err)
	}
	// Sweep to MaxInt64: refill saturates without overflow, generation bumps once.
	g, err := r.Sweep(math.MaxInt64)
	if err != nil || g != 2 {
		t.Fatalf("sweep=(%d,%v)", g, err)
	}
	s, _ := r.Inspect("big", math.MaxInt64)
	if s.Tokens != math.MaxInt64 || s.LastRefill != math.MaxInt64 || s.NextRefill != 0 {
		t.Fatalf("s=%+v", s)
	}
	// No-op sweep keeps generation.
	g, err = r.Sweep(math.MaxInt64)
	if err != nil || g != 2 {
		t.Fatalf("noop=(%d,%v)", g, err)
	}
}

func TestSweepMultiBucketUniformTime(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 10, 0), acquire("b", 20, 0)}); err != nil {
		t.Fatal(err)
	}
	g, err := r.Sweep(14)
	if err != nil || g != 2 {
		t.Fatalf("sweep=(%d,%v)", g, err)
	}
	s := r.Snapshot()
	if s.Buckets[0].Tokens != 6 || s.Buckets[0].LastRefill != 10 || s.Buckets[0].LastObserved != 14 {
		t.Fatalf("a=%+v", s.Buckets[0])
	}
	if s.Buckets[1].Tokens != 4 || s.Buckets[1].LastRefill != 14 || s.Buckets[1].LastObserved != 14 {
		t.Fatalf("b=%+v", s.Buckets[1])
	}
}

func TestUnknownBucketAndValidationOrder(t *testing.T) {
	r := newRegistry(t)
	// Unknown bucket only surfaces after structural validation passes.
	if _, err := r.ApplyBatch([]Change{acquire("nope", 1, 0)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	// Structural error later in the batch beats earlier unknown bucket.
	if _, err := r.ApplyBatch([]Change{acquire("nope", 1, 0), acquire("a", -1, 0)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if _, err := r.Inspect("nope", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inspect=%v", err)
	}
	if _, err := r.Inspect("a", -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("inspect=%v", err)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	r := newRegistry(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				at := int64(j)
				_, _ = r.ApplyBatch([]Change{acquire("a", 1, at)})
				_, _ = r.ApplyBatch([]Change{refund("a", 1, at)})
				_, _ = r.Inspect("b", at)
				_, _ = r.Sweep(at)
				_ = r.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Buckets) != 2 || s.Buckets[0].Name != "a" || s.Buckets[1].Name != "b" {
		t.Fatalf("snapshot=%+v", s)
	}
	for _, b := range s.Buckets {
		if b.Tokens < 0 || b.Tokens > b.Capacity {
			t.Fatalf("tokens out of range: %+v", b)
		}
	}
}
