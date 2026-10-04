package reservation

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchReplaceSameID(t *testing.T) {
	l := newLedger(t)
	if _, e := l.Apply(add("a", "r", 0, 5, []byte("v1"))); e != nil {
		t.Fatal(e)
	}
	// Delete then re-add the same ID within one batch must succeed.
	g, e := l.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		add("a", "r", 5, 9, []byte("v2")),
	})
	if e != nil || g != 2 {
		t.Fatalf("replace=(%d,%v)", g, e)
	}
	r, e := l.At("r", 7)
	if e != nil || !r.Found || string(r.Reservation.Value) != "v2" {
		t.Fatalf("at=%+v %v", r, e)
	}
	if r.Generation != 2 {
		t.Fatalf("generation=%d", r.Generation)
	}
	// Duplicate within the same batch fails and rolls back.
	before := l.Snapshot()
	if _, e = l.ApplyBatch([]Change{add("b", "r", 20, 30, nil), add("b", "r", 40, 50, nil)}); !errors.Is(e, ErrDuplicate) {
		t.Fatalf("dup=%v", e)
	}
	if !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatal("duplicate batch mutated state")
	}
}

func TestConflictRollbackKeepsGeneration(t *testing.T) {
	l := newLedger(t)
	if _, e := l.Apply(add("a", "r", 0, 10, nil)); e != nil {
		t.Fatal(e)
	}
	g, e := l.Apply(add("b", "r", 5, 15, nil))
	if !errors.Is(e, ErrConflict) || g != 1 {
		t.Fatalf("conflict=(%d,%v)", g, e)
	}
	// Empty batch returns current generation without advancing.
	g, e = l.ApplyBatch(nil)
	if e != nil || g != 1 {
		t.Fatalf("empty=(%d,%v)", g, e)
	}
	// Successful non-empty batch advances generation exactly once even if
	// content ends up identical (delete + re-add same data).
	g, e = l.ApplyBatch([]Change{{Type: ChangeDelete, ID: "a"}, add("a", "r", 0, 10, nil)})
	if e != nil || g != 2 {
		t.Fatalf("rewrite=(%d,%v)", g, e)
	}
}

func TestExtremeIntervals(t *testing.T) {
	l, e := New(Options{MaxResources: 2, MaxReservations: 8, MaxValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	_, e = l.ApplyBatch([]Change{
		add("lo", "r", math.MinInt64, math.MinInt64+1, nil),
		add("hi", "r", math.MaxInt64-1, math.MaxInt64, nil),
	})
	if e != nil {
		t.Fatal(e)
	}
	// Adjacent to existing extremes is allowed.
	if _, e = l.Apply(add("mid-adj", "r", math.MinInt64+1, math.MinInt64+2, nil)); e != nil {
		t.Fatal(e)
	}
	// Overlapping the extreme interval conflicts.
	if _, e = l.Apply(add("bad", "r", math.MinInt64, math.MinInt64+1, nil)); !errors.Is(e, ErrConflict) {
		t.Fatalf("overlap=%v", e)
	}
	r, e := l.At("r", math.MaxInt64-1)
	if e != nil || !r.Found || r.Reservation.ID != "hi" {
		t.Fatalf("at max=%+v %v", r, e)
	}
	s, e := l.Scan("r", math.MinInt64, math.MaxInt64, 1000)
	if e != nil || len(s.Reservations) != 3 {
		t.Fatalf("scan=%+v %v", s, e)
	}
	// Scan window touching only the boundary excludes the interval.
	s, e = l.Scan("r", math.MinInt64+1, math.MinInt64+2, 10)
	if e != nil || len(s.Reservations) != 1 || s.Reservations[0].ID != "mid-adj" {
		t.Fatalf("boundary scan=%+v %v", s, e)
	}
}

func TestValueOwnership(t *testing.T) {
	l := newLedger(t)
	v := []byte("abc")
	if _, e := l.Apply(add("a", "r", 0, 10, v)); e != nil {
		t.Fatal(e)
	}
	v[0] = 'X' // mutating caller input must not affect stored state
	r, _ := l.At("r", 1)
	if string(r.Reservation.Value) != "abc" {
		t.Fatal("input aliased")
	}
	r.Reservation.Value[1] = 'Y' // mutating At output must not affect state
	s, _ := l.Scan("r", 0, 10, 1)
	if string(s.Reservations[0].Value) != "abc" {
		t.Fatal("At output aliased")
	}
	s.Reservations[0].Value[2] = 'Z'
	snap := l.Snapshot()
	if string(snap.Resources[0].Reservations[0].Value) != "abc" {
		t.Fatal("Scan output aliased")
	}
	snap.Resources[0].Reservations[0].Value[0] = 'W'
	r2, _ := l.At("r", 1)
	if string(r2.Reservation.Value) != "abc" {
		t.Fatal("Snapshot output aliased")
	}
}

func TestConcurrentAccess(t *testing.T) {
	l, e := New(Options{MaxResources: 64, MaxReservations: 4096, MaxValueBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			id := string(rune('a'+w)) + "-id"
			res := string(rune('a' + w%4))
			for i := 0; i < 200; i++ {
				_, _ = l.Apply(add(id, res, int64(2*i), int64(2*i+1), []byte("v")))
				_, _ = l.At(res, int64(2*i))
				_, _ = l.Scan(res, 0, 1000, 10)
				_ = l.Snapshot()
				_, _ = l.ApplyBatch([]Change{{Type: ChangeDelete, ID: id}})
			}
		}(w)
	}
	wg.Wait()
	s := l.Snapshot()
	if s.UsedReservations != 0 || len(s.Resources) != 0 {
		t.Fatalf("final state not empty: %+v", s)
	}
}

func TestScanLimitAndUnknownResource(t *testing.T) {
	l := newLedger(t)
	_, _ = l.ApplyBatch([]Change{add("a", "r", 0, 5, nil), add("b", "r", 5, 10, nil)})
	s, e := l.Scan("r", 0, 10, 1)
	if e != nil || len(s.Reservations) != 1 || s.Reservations[0].ID != "a" {
		t.Fatalf("limit=%+v %v", s, e)
	}
	s, e = l.Scan("unknown", 0, 10, 10)
	if e != nil || len(s.Reservations) != 0 {
		t.Fatalf("unknown=%+v %v", s, e)
	}
	if _, e = l.Scan("bad/r", 0, 10, 10); !errors.Is(e, ErrInvalidResource) {
		t.Fatalf("resource=%v", e)
	}
	if _, e = l.Scan("r", 0, 10, 1001); !errors.Is(e, ErrInvalidScan) {
		t.Fatalf("limit=%v", e)
	}
	if _, e = l.At("bad/r", 0); !errors.Is(e, ErrInvalidResource) {
		t.Fatalf("at=%v", e)
	}
}
