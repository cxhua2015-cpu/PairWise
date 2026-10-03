package join

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestWatermarkEqualIsNoOpProgress(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(wm(Left, 5)); err != nil {
		t.Fatal(err)
	}
	o, err := j.Apply(wm(Left, 5))
	if err != nil || len(o.Expired) != 0 {
		t.Fatalf("equal watermark: %+v %v", o, err)
	}
}

func TestLateBoundaries(t *testing.T) {
	j := mustNew(t, Options{Window: 5, MaxEvents: 10, MaxBytes: 1000})
	if _, err := j.Apply(wm(Right, 20)); err != nil {
		t.Fatal(err)
	}
	// right wm == left.Time + Window is allowed
	if _, err := j.Apply(ev(Left, "k", "ok", 15, "")); err != nil {
		t.Fatalf("boundary should be accepted: %v", err)
	}
	if _, err := j.Apply(ev(Left, "k", "late", 14, "")); !errors.Is(err, ErrLate) {
		t.Fatalf("want ErrLate, got %v", err)
	}
	// same-side watermark lateness
	if _, err := j.Apply(wm(Left, 30)); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Left, "k", "late2", 29, "")); !errors.Is(err, ErrLate) {
		t.Fatalf("want ErrLate, got %v", err)
	}
	if _, err := j.Apply(ev(Left, "k", "ok2", 30, "")); err != nil {
		t.Fatalf("equal to wm allowed: %v", err)
	}
}

func TestExpiryOrderingAndOnlyNewlyExpired(t *testing.T) {
	j := mustNew(t, Options{Window: 2, MaxEvents: 10, MaxBytes: 1000})
	for _, u := range []Update{
		ev(Left, "b", "i2", 3, ""), ev(Left, "a", "i1", 3, ""), ev(Left, "a", "i0", 1, ""),
	} {
		if _, err := j.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	o, err := j.Apply(wm(Right, 4)) // expires time<2 -> only i0
	if err != nil || len(o.Expired) != 1 || o.Expired[0].ID != "i0" {
		t.Fatalf("first expiry: %+v %v", o, err)
	}
	o, err = j.Apply(wm(Right, 6)) // expires time<4 -> i1,i2 sorted by time,key,id
	if err != nil || len(o.Expired) != 2 || o.Expired[0].ID != "i1" || o.Expired[1].ID != "i2" {
		t.Fatalf("second expiry: %+v %v", o, err)
	}
	o, err = j.Apply(wm(Right, 100))
	if err != nil || len(o.Expired) != 0 {
		t.Fatalf("no re-expiry: %+v %v", o, err)
	}
}

func TestMatchOrderingByOppositeTimeID(t *testing.T) {
	j := mustNew(t, Options{Window: 100, MaxEvents: 10, MaxBytes: 1000})
	for _, u := range []Update{
		ev(Right, "k", "b", 5, ""), ev(Right, "k", "a", 5, ""), ev(Right, "k", "z", 1, ""),
	} {
		if _, err := j.Apply(u); err != nil {
			t.Fatal(err)
		}
	}
	o, err := j.Apply(ev(Left, "k", "l", 5, ""))
	if err != nil || len(o.Matches) != 3 {
		t.Fatalf("%+v %v", o, err)
	}
	got := []string{o.Matches[0].RightID, o.Matches[1].RightID, o.Matches[2].RightID}
	want := []string{"z", "a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v want %v", got, want)
		}
	}
	// window edge: |dt| == window matches
	o, err = j.Apply(ev(Left, "k", "l2", 105, ""))
	if err != nil || len(o.Matches) != 2 { // right times 5,5 within 100; 1 is not
		t.Fatalf("edge: %+v %v", o, err)
	}
}

func TestBatchAtomicityOnLateAndInvalid(t *testing.T) {
	j := mustNew(t, baseOptions())
	if _, err := j.Apply(wm(Left, 50)); err != nil {
		t.Fatal(err)
	}
	before := j.Snapshot()
	out, err := j.ApplyBatch([]Update{ev(Right, "k", "r", 60, "x"), ev(Left, "k", "late", 1, "")})
	if !errors.Is(err, ErrLate) || out != nil {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if s := j.Snapshot(); s.Count != before.Count || s.Bytes != before.Bytes {
		t.Fatalf("mutated: %+v", s)
	}
	if _, err := j.ApplyBatch([]Update{ev(Right, "k", "r", 60, "x"), {}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid in batch: %v", err)
	}
	if j.Snapshot().Count != 0 {
		t.Fatal("invalid batch mutated state")
	}
}

func TestCapacityBytesExactBoundary(t *testing.T) {
	j := mustNew(t, Options{Window: 1, MaxEvents: 5, MaxBytes: 4})
	if _, err := j.Apply(ev(Left, "k", "i", 0, "xy")); err != nil { // exactly 4
		t.Fatal(err)
	}
	if _, err := j.Apply(ev(Right, "k", "j", 0, "")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("over by one byte: %v", err)
	}
}

func TestConflictAfterExpiryReuseAcrossBatch(t *testing.T) {
	j := mustNew(t, Options{Window: 2, MaxEvents: 10, MaxBytes: 1000})
	out, err := j.ApplyBatch([]Update{
		ev(Left, "k", "id", 1, "a"),
		wm(Right, 10),               // expires it
		ev(Left, "k", "id", 9, "b"), // reuse in same batch
	})
	if err != nil || len(out) != 3 {
		t.Fatalf("%+v %v", out, err)
	}
	if s := j.Snapshot(); s.Count != 1 || s.Events[0].Time != 9 {
		t.Fatalf("snap=%+v", s)
	}
}

func TestConcurrentMixedLoad(t *testing.T) {
	j := mustNew(t, Options{Window: 10, MaxEvents: 100000, MaxBytes: 10000000})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				side := Left
				if (g+n)%2 == 1 {
					side = Right
				}
				id := fmt.Sprintf("g%d-n%d", g, n)
				_, err := j.Apply(ev(side, fmt.Sprintf("k%d", n%7), id, int64(n), "p"))
				if err != nil && !errors.Is(err, ErrLate) {
					t.Errorf("apply: %v", err)
					return
				}
				if n%5 == 0 {
					if _, err := j.ApplyBatch([]Update{wm(side, int64(n))}); err != nil && !errors.Is(err, ErrTime) {
						t.Errorf("wm: %v", err)
						return
					}
				}
				_ = j.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := j.Snapshot()
	if s.Count != len(s.Events) {
		t.Fatalf("count %d != events %d", s.Count, len(s.Events))
	}
	sum := 0
	for _, e := range s.Events {
		sum += e.Bytes
	}
	if sum != s.Bytes {
		t.Fatalf("bytes %d != sum %d", s.Bytes, sum)
	}
}

func TestSnapshotConsistencyUnderConcurrency(t *testing.T) {
	j := mustNew(t, Options{Window: 1000, MaxEvents: 100000, MaxBytes: 10000000})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s := j.Snapshot()
				if s.Count != len(s.Events) {
					t.Errorf("inconsistent snapshot")
					return
				}
			}
		}
	}()
	for n := 0; n < 500; n++ {
		if _, err := j.Apply(ev(Left, "k", fmt.Sprintf("id%d", n), int64(n), "v")); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}
