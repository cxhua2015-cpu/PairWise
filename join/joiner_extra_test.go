package join

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestBoundaryArithmeticAtMaxTime(t *testing.T) {
	const maxT = 1_000_000_000_000
	j := mustNew(t, Options{Window: maxT, MaxEvents: 10, MaxBytes: 1000})
	// Opposite watermark equal to time+window boundary (2e12 would overflow int32
	// and naive int64 sums if the bound checks were wrong) must not be late.
	if _, err := j.Apply(wm(Right, maxT)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "k", "l", 0, "")); err != nil {
		t.Fatalf("boundary lateness: %v", err)
	}
	// Event at max time with max window: sum 2e12 must not overflow.
	if _, err := j.Apply(ev(Right, "k", "r", maxT, "")); err != nil {
		t.Fatalf("max time event: %v", err)
	}
	o, err := j.Apply(wm(Left, maxT))
	if err != nil {
		t.Fatal(err)
	}
	// right r: maxT + maxT >= maxT, retained.
	if len(o.Expired) != 0 || j.Snapshot().Count != 2 {
		t.Fatalf("expiry at max boundary: %+v count=%d", o, j.Snapshot().Count)
	}
}

func TestLateViaOppositeWatermark(t *testing.T) {
	j := mustNew(t, Options{Window: 5, MaxEvents: 10, MaxBytes: 100})
	if _, err := j.Apply(wm(Right, 20)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "k", "a", 14, "")); !errors.Is(err, ErrLate) {
		t.Fatalf("14+5<20 must be late: %v", err)
	}
	if _, err := j.Apply(ev(Left, "k", "b", 15, "")); err != nil {
		t.Fatalf("15+5==20 boundary must pass: %v", err)
	}
	if _, err := j.Apply(wm(Left, 16)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "k", "c", 15, "")); !errors.Is(err, ErrLate) {
		t.Fatalf("below own watermark must be late: %v", err)
	}
}

func TestExpiredOrderingAndSingleReport(t *testing.T) {
	j := mustNew(t, Options{Window: 2, MaxEvents: 10, MaxBytes: 1000})
	for _, u := range []Update{
		ev(Left, "b", "i2", 5, ""), ev(Left, "a", "i1", 5, ""),
		ev(Left, "a", "i0", 3, ""), ev(Left, "z", "i9", 20, ""),
	} {
		if _, err := j.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	o, err := j.Apply(wm(Right, 8))
	if err != nil {
		t.Fatal(err)
	}
	want := []Expired{
		{Side: Left, Key: "a", ID: "i0", Time: 3},
		{Side: Left, Key: "a", ID: "i1", Time: 5},
		{Side: Left, Key: "b", ID: "i2", Time: 5},
	}
	if !reflect.DeepEqual(o.Expired, want) {
		t.Fatalf("expired order: %+v", o.Expired)
	}
	// Re-advancing must not re-report already expired events.
	o2, err := j.Apply(wm(Right, 30))
	if err != nil || len(o2.Expired) != 1 || o2.Expired[0].ID != "i9" {
		t.Fatalf("second expiry: %+v %v", o2, err)
	}
	// Equal watermark is a legal no-op.
	o3, err := j.Apply(wm(Right, 30))
	if err != nil || len(o3.Expired) != 0 {
		t.Fatalf("equal watermark: %+v %v", o3, err)
	}
}

func TestMatchOrderingByOppositeTimeThenID(t *testing.T) {
	j := mustNew(t, Options{Window: 100, MaxEvents: 10, MaxBytes: 1000})
	for _, u := range []Update{
		ev(Right, "k", "rb", 10, ""), ev(Right, "k", "ra", 10, ""), ev(Right, "k", "rc", 4, ""),
	} {
		if _, err := j.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	o, err := j.Apply(ev(Left, "k", "l", 10, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, m := range o.Matches {
		got = append(got, m.RightID)
	}
	if !reflect.DeepEqual(got, []string{"rc", "ra", "rb"}) {
		t.Fatalf("match order: %v", got)
	}
}

func TestFailedBatchLeavesNoTrace(t *testing.T) {
	j := mustNew(t, Options{Window: 5, MaxEvents: 10, MaxBytes: 1000})
	if _, err := j.Apply(ev(Right, "k", "r", 10, "R")); err != nil {
		t.Fatal(err)
	}
	before := j.Snapshot()
	// Invalid update mid-batch after a watermark advance and a match.
	out, err := j.ApplyBatch([]Update{
		wm(Left, 3),
		ev(Left, "k", "l", 10, "L"),
		ev(Left, "", "bad", 1, ""),
	})
	if !errors.Is(err, ErrInvalid) || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if !reflect.DeepEqual(before, j.Snapshot()) {
		t.Fatalf("state mutated: %+v", j.Snapshot())
	}
	// The prepared match must still be producible on retry.
	o, err := j.Apply(ev(Left, "k", "l", 10, "L"))
	if err != nil || len(o.Matches) != 1 || string(o.Matches[0].Right) != "R" {
		t.Fatalf("retry: %+v %v", o, err)
	}
}

func TestApplyEqualsSingleElementBatch(t *testing.T) {
	a := mustNew(t, Options{Window: 5, MaxEvents: 10, MaxBytes: 100})
	b := mustNew(t, Options{Window: 5, MaxEvents: 10, MaxBytes: 100})
	u := ev(Left, "k", "i", 1, "p")
	oa, errA := a.Apply(u)
	obs, errB := b.ApplyBatch([]Update{u})
	if errA != nil || errB != nil || len(obs) != 1 || !reflect.DeepEqual(oa, obs[0]) {
		t.Fatalf("apply=%+v,%v batch=%+v,%v", oa, errA, obs, errB)
	}
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
		t.Fatal("snapshots differ")
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	j := mustNew(t, Options{Window: 50, MaxEvents: 4096, MaxBytes: 1 << 20})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				side := Left
				if (g+i)%2 == 1 {
					side = Right
				}
				id := fmt.Sprintf("g%d-i%d", g, i)
				_, err := j.Apply(ev(side, fmt.Sprintf("key-%d", i%7), id, int64(i), "payload"))
				if errors.Is(err, ErrLate) {
					// Watermarks from other goroutines may legitimately
					// make an event late; that is fine for this race test.
				} else if err != nil {
					t.Errorf("apply: %v", err)
					return
				}
				if i%10 == 0 {
					_, _ = j.ApplyBatch([]Update{wm(side, int64(i))})
				}
				s := j.Snapshot()
				if s.Count != len(s.Events) {
					t.Errorf("inconsistent snapshot: %d vs %d", s.Count, len(s.Events))
					return
				}
			}
		}()
	}
	wg.Wait()
	s := j.Snapshot()
	sum := 0
	for _, e := range s.Events {
		sum += e.Bytes
	}
	if sum != s.Bytes {
		t.Fatalf("bytes mismatch: %d vs %d", sum, s.Bytes)
	}
}
