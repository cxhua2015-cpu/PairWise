package tokenbucket

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func options() Options { return Options{MaxBuckets: 16, MaxNameBytes: 16} }
func specs() []BucketSpec {
	return []BucketSpec{{Name: "a", Capacity: 10, RefillTokens: 3, RefillEvery: 5}, {Name: "b", Capacity: 20, RefillTokens: 2, RefillEvery: 7}}
}
func newRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := New(options(), specs())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func acquire(bucket string, tokens, at int64) Change {
	return Change{Kind: Acquire, Bucket: bucket, Tokens: tokens, At: at}
}
func refund(bucket string, tokens, at int64) Change {
	return Change{Kind: Refund, Bucket: bucket, Tokens: tokens, At: at}
}

func TestOptionsAndStructuralValidationFirst(t *testing.T) {
	bad := []struct {
		o Options
		s []BucketSpec
	}{{Options{}, specs()}, {options(), nil}, {Options{MaxBuckets: 1, MaxNameBytes: 8}, specs()}, {options(), []BucketSpec{{Name: "a", Capacity: 1, RefillTokens: 1, RefillEvery: 1}, {Name: "a", Capacity: 2, RefillTokens: 1, RefillEvery: 1}}}, {options(), []BucketSpec{{Name: "bad/x", Capacity: 1, RefillTokens: 1, RefillEvery: 1}}}}
	for _, tc := range bad {
		if _, err := New(tc.o, tc.s); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("New=%v", err)
		}
	}
	r := newRegistry(t)
	before := r.Snapshot()
	_, err := r.ApplyBatch([]Change{acquire("a", 100, 0), {Kind: 99, Bucket: "a", Tokens: 1, At: 0}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
	_, err = r.ApplyBatch([]Change{acquire("bad/x", 1, 0)})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("name=%v", err)
	}
}

func TestRefillBoundariesAndInspectNoMutation(t *testing.T) {
	r := newRegistry(t)
	_, err := r.ApplyBatch([]Change{acquire("a", 8, 0)})
	if err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	s, err := r.Inspect("a", 4)
	if err != nil || s.Tokens != 2 || s.LastRefill != 0 || s.NextRefill != 5 {
		t.Fatalf("at4=%+v %v", s, err)
	}
	s, err = r.Inspect("a", 5)
	if err != nil || s.Tokens != 5 || s.LastRefill != 5 || s.NextRefill != 10 {
		t.Fatalf("at5=%+v %v", s, err)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatal("Inspect mutated")
	}
}

func TestBatchRollbackAcrossBucketsAndTime(t *testing.T) {
	r := newRegistry(t)
	before := r.Snapshot()
	_, err := r.ApplyBatch([]Change{acquire("a", 8, 0), acquire("b", 21, 0)})
	if !errors.Is(err, ErrInsufficient) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("rollback=%v", err)
	}
	_, _ = r.ApplyBatch([]Change{acquire("a", 1, 10)})
	before = r.Snapshot()
	_, err = r.ApplyBatch([]Change{acquire("b", 1, 20), acquire("a", 1, 9)})
	if !errors.Is(err, ErrTimeBackwards) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("time rollback=%v", err)
	}
}

func TestRefundLimitAndGeneration(t *testing.T) {
	r := newRegistry(t)
	_, _ = r.ApplyBatch([]Change{acquire("a", 4, 0)})
	g, err := r.ApplyBatch([]Change{refund("a", 4, 0)})
	if err != nil || g != 2 {
		t.Fatalf("refund=(%d,%v)", g, err)
	}
	before := r.Snapshot()
	_, err = r.ApplyBatch([]Change{refund("a", 1, 0)})
	if !errors.Is(err, ErrOverflow) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("overflow=%v", err)
	}
	g, err = r.ApplyBatch(nil)
	if err != nil || g != 2 {
		t.Fatalf("empty=(%d,%v)", g, err)
	}
}

func TestSweepStableOrderAndNoop(t *testing.T) {
	r := newRegistry(t)
	g, err := r.Sweep(5)
	if err != nil || g != 1 {
		t.Fatalf("sweep=(%d,%v)", g, err)
	}
	s := r.Snapshot()
	if len(s.Buckets) != 2 || s.Buckets[0].Name != "a" || s.Buckets[1].Name != "b" || s.Buckets[0].LastRefill != 5 || s.Buckets[1].LastRefill != 0 {
		t.Fatalf("snapshot=%+v", s)
	}
	g, err = r.Sweep(5)
	if err != nil || g != 1 {
		t.Fatalf("noop=(%d,%v)", g, err)
	}
	before := r.Snapshot()
	_, err = r.Sweep(4)
	if !errors.Is(err, ErrTimeBackwards) || !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("backwards=%v", err)
	}
}

func TestExtremeArithmetic(t *testing.T) {
	r, err := New(options(), []BucketSpec{{Name: "huge", Capacity: math.MaxInt64, RefillTokens: math.MaxInt64, RefillEvery: 1}, {Name: "slow", Capacity: 10, RefillTokens: 3, RefillEvery: math.MaxInt64}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.ApplyBatch([]Change{acquire("huge", math.MaxInt64, 0), acquire("slow", 10, 0)})
	h, err := r.Inspect("huge", math.MaxInt64)
	if err != nil || h.Tokens != math.MaxInt64 || h.LastRefill != math.MaxInt64 || h.NextRefill != 0 {
		t.Fatalf("huge=%+v %v", h, err)
	}
	s, err := r.Inspect("slow", math.MaxInt64)
	if err != nil || s.Tokens != 3 || s.LastRefill != math.MaxInt64 || s.NextRefill != 0 {
		t.Fatalf("slow=%+v %v", s, err)
	}
}

func TestConcurrentIndependentBuckets(t *testing.T) {
	list := make([]BucketSpec, 32)
	for i := range list {
		list[i] = BucketSpec{Name: fmt.Sprintf("b-%d", i), Capacity: 2, RefillTokens: 1, RefillEvery: 1}
	}
	r, _ := New(Options{MaxBuckets: 32, MaxNameBytes: 16}, list)
	var wg sync.WaitGroup
	for i := range list {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("b-%d", i)
			if _, err := r.ApplyBatch([]Change{acquire(name, 1, 0)}); err != nil {
				t.Errorf("acquire=%v", err)
			}
			_, _ = r.Inspect(name, 1)
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	if s := r.Snapshot(); s.Generation != 32 || len(s.Buckets) != 32 {
		t.Fatalf("snapshot=%+v", s)
	}
}
