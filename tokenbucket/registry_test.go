package tokenbucket

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchRollbackRefillAndTimeFields(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 10, 0)}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	// Refill would occur at At=10 for bucket a, then the batch fails on b.
	_, err := r.ApplyBatch([]Change{acquire("a", 1, 10), acquire("b", 100, 0)})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("rollback leaked refill: %+v", r.Snapshot())
	}
}

func TestCrossBucketIsolation(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 10, 0)}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	// b untouched by a's acquire; a's clock advanced only for a.
	var a, b BucketState
	for _, bs := range s.Buckets {
		if bs.Name == "a" {
			a = bs
		} else {
			b = bs
		}
	}
	if a.Tokens != 0 || b.Tokens != 20 || b.LastObserved != 0 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	if _, err := r.ApplyBatch([]Change{acquire("b", 20, 0)}); err != nil {
		t.Fatalf("b independent: %v", err)
	}
}

func TestRefillPartialPeriodsAndCap(t *testing.T) {
	r := newRegistry(t)
	// a: cap 10, +3 per 5.
	if _, err := r.ApplyBatch([]Change{acquire("a", 9, 0)}); err != nil {
		t.Fatal(err)
	}
	s, err := r.Inspect("a", 9) // one full period
	if err != nil || s.Tokens != 4 || s.LastRefill != 5 || s.LastObserved != 9 {
		t.Fatalf("at9=%+v %v", s, err)
	}
	s, err = r.Inspect("a", 100) // many periods, capped
	if err != nil || s.Tokens != 10 || s.LastRefill != 100 || s.NextRefill != 0 {
		t.Fatalf("at100=%+v %v", s, err)
	}
	// Refill actually commits via a batch.
	g, err := r.ApplyBatch([]Change{refund("a", 3, 9)})
	if err != nil || g != 2 {
		t.Fatalf("refund=(%d,%v)", g, err)
	}
	if st := r.Snapshot().Buckets[0]; st.Tokens != 7 || st.LastRefill != 5 {
		t.Fatalf("state=%+v", st)
	}
}

func TestRefundCapBoundary(t *testing.T) {
	r := newRegistry(t)
	if _, err := r.ApplyBatch([]Change{acquire("a", 10, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyBatch([]Change{refund("a", 10, 0)}); err != nil {
		t.Fatalf("exact fill: %v", err)
	}
	if _, err := r.ApplyBatch([]Change{refund("a", 1, 0)}); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow=%v", err)
	}
}

func TestExtremeNoOverflow(t *testing.T) {
	r, err := New(options(), []BucketSpec{
		{Name: "max", Capacity: math.MaxInt64, RefillTokens: math.MaxInt64, RefillEvery: math.MaxInt64},
		{Name: "one", Capacity: math.MaxInt64, RefillTokens: 1, RefillEvery: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyBatch([]Change{acquire("max", math.MaxInt64, 0), acquire("one", math.MaxInt64, 0)}); err != nil {
		t.Fatal(err)
	}
	m, err := r.Inspect("max", math.MaxInt64)
	if err != nil || m.Tokens != math.MaxInt64 || m.LastRefill != math.MaxInt64 || m.NextRefill != 0 {
		t.Fatalf("max=%+v %v", m, err)
	}
	o, err := r.Inspect("one", math.MaxInt64)
	if err != nil || o.Tokens != math.MaxInt64 || o.LastRefill != math.MaxInt64 || o.NextRefill != 0 {
		t.Fatalf("one=%+v %v", o, err)
	}
	// Refund overflow at capacity extremes.
	if _, err := r.ApplyBatch([]Change{refund("max", 1, math.MaxInt64)}); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow=%v", err)
	}
	// Acquire full capacity at extreme time succeeds after refill.
	g, err := r.ApplyBatch([]Change{acquire("max", math.MaxInt64, math.MaxInt64)})
	if err != nil || g != 2 {
		t.Fatalf("acquire=(%d,%v)", g, err)
	}
}

func TestSweepAdvanceAndValidation(t *testing.T) {
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
	if _, err := r.Sweep(-1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("neg=%v", err)
	}
	if _, err := r.Sweep(13); !errors.Is(err, ErrTimeBackwards) {
		t.Fatalf("back=%v", err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	r := newRegistry(t)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "a"
			if i%2 == 1 {
				name = "b"
			}
			at := int64(i/2) * 5
			_, _ = r.ApplyBatch([]Change{acquire(name, 1, at)})
			_, _ = r.Inspect(name, at+5)
			_, _ = r.Sweep(at)
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if len(s.Buckets) != 2 || s.Buckets[0].Name != "a" || s.Buckets[1].Name != "b" {
		t.Fatalf("snapshot=%+v", s)
	}
}
