package reservation

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestBatchReplaceSameID(t *testing.T) {
	l := newLedger(t)
	g, err := l.Apply(add("a", "r", 0, 10, []byte("v1")))
	if err != nil || g != 1 {
		t.Fatalf("apply=(%d,%v)", g, err)
	}
	g, err = l.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		add("a", "r", 20, 30, []byte("v2")),
	})
	if err != nil || g != 2 {
		t.Fatalf("replace=(%d,%v)", g, err)
	}
	r, err := l.At("r", 25)
	if err != nil || !r.Found || string(r.Reservation.Value) != "v2" {
		t.Fatalf("at=%+v %v", r, err)
	}
	if r.Generation != 2 {
		t.Fatalf("generation=%d", r.Generation)
	}
	if _, err = l.Apply(Change{Type: ChangeDelete, ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete=%v", err)
	}
	if _, err = l.Apply(add("a", "r", 100, 200, nil)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("dup=%v", err)
	}
}

func TestConflictRollbackKeepsState(t *testing.T) {
	l := newLedger(t)
	if _, err := l.Apply(add("a", "r", 0, 10, []byte("keep"))); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	_, err := l.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		add("b", "r", 0, 5, nil),
		add("c", "r", 4, 9, nil),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	after := l.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed after rollback: %+v", after)
	}
	if after.Generation != 1 {
		t.Fatalf("generation advanced: %d", after.Generation)
	}
	if _, err = l.Apply(add("b", "r", 10, 20, nil)); err != nil {
		t.Fatalf("adjacent=%v", err)
	}
}

func TestCapacityRollbackAndValueBytes(t *testing.T) {
	l, err := New(Options{MaxResources: 2, MaxReservations: 3, MaxValueBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Apply(add("a", "r1", 0, 1, []byte("ab"))); err != nil {
		t.Fatal(err)
	}
	before := l.Snapshot()
	if _, err = l.Apply(add("b", "r1", 1, 2, []byte("cde"))); !errors.Is(err, ErrCapacity) {
		t.Fatalf("valueBytes=%v", err)
	}
	if _, err = l.ApplyBatch([]Change{
		add("b", "r2", 0, 1, nil),
		add("c", "r3", 0, 1, nil),
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("resources=%v", err)
	}
	if _, err = l.ApplyBatch([]Change{
		add("b", "r1", 1, 2, nil),
		add("c", "r1", 2, 3, nil),
		add("d", "r1", 3, 4, nil),
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("reservations=%v", err)
	}
	if after := l.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed: %+v", after)
	}
	if _, err = l.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		add("a", "r1", 0, 1, []byte("abcd")),
	}); err != nil {
		t.Fatalf("re-add=%v", err)
	}
	if s := l.Snapshot(); s.UsedValueBytes != 4 || s.UsedReservations != 1 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestExtremeBoundaries(t *testing.T) {
	l := newLedger(t)
	_, err := l.ApplyBatch([]Change{
		add("lo", "r", math.MinInt64, math.MinInt64+1, nil),
		add("hi", "r", math.MaxInt64-1, math.MaxInt64, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at    int64
		found bool
		id    string
	}{
		{math.MinInt64, true, "lo"},
		{math.MinInt64 + 1, false, ""},
		{math.MaxInt64 - 1, true, "hi"},
		{math.MaxInt64, false, ""},
	} {
		r, err := l.At("r", tc.at)
		if err != nil || r.Found != tc.found || r.Reservation.ID != tc.id {
			t.Errorf("At(%d)=%+v %v", tc.at, r, err)
		}
	}
	s, err := l.Scan("r", math.MinInt64, math.MaxInt64, 1000)
	if err != nil || len(s.Reservations) != 2 {
		t.Fatalf("scan=%+v %v", s, err)
	}
	s, err = l.Scan("unknown", 0, 100, 10)
	if err != nil || len(s.Reservations) != 0 {
		t.Fatalf("unknown scan=%+v %v", s, err)
	}
	r, err := l.At("unknown", 0)
	if err != nil || r.Found {
		t.Fatalf("unknown at=%+v %v", r, err)
	}
}

func TestScanWindowAndLimit(t *testing.T) {
	l := newLedger(t)
	for i := 0; i < 5; i++ {
		if _, err := l.Apply(add(fmt.Sprintf("r%d", i), "res", int64(i*10), int64(i*10+10), nil)); err != nil {
			t.Fatal(err)
		}
	}
	s, err := l.Scan("res", 10, 20, 10)
	if err != nil || len(s.Reservations) != 1 || s.Reservations[0].ID != "r1" {
		t.Fatalf("scan=%+v %v", s, err)
	}
	s, err = l.Scan("res", 0, 50, 2)
	if err != nil || len(s.Reservations) != 2 ||
		s.Reservations[0].ID != "r0" || s.Reservations[1].ID != "r1" {
		t.Fatalf("limit scan=%+v %v", s, err)
	}
	if _, err = l.Scan("res", 0, 10, 1001); !errors.Is(err, ErrInvalidScan) {
		t.Fatalf("limit=%v", err)
	}
	if _, err = l.Scan("bad/res", 0, 10, 1); !errors.Is(err, ErrInvalidResource) {
		t.Fatalf("resource=%v", err)
	}
}

func TestValueOwnershipIsolation(t *testing.T) {
	l := newLedger(t)
	in := []byte("in")
	if _, err := l.Apply(add("a", "r", 0, 10, in)); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	r, _ := l.At("r", 0)
	if string(r.Reservation.Value) != "in" {
		t.Fatal("input aliases state")
	}
	r.Reservation.Value[0] = 'Y'
	s, _ := l.Scan("r", 0, 10, 1)
	s.Reservations[0].Value[1] = 'Z'
	snap := l.Snapshot()
	snap.Resources[0].Reservations[0].Value[0] = 'W'
	r2, _ := l.At("r", 0)
	if string(r2.Reservation.Value) != "in" {
		t.Fatalf("state mutated: %q", r2.Reservation.Value)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	l := newLedger(t)
	g, err := l.ApplyBatch(nil)
	if err != nil || g != 0 {
		t.Fatalf("empty=(%d,%v)", g, err)
	}
	if _, err = l.Apply(add("a", "r", 0, 1, nil)); err != nil {
		t.Fatal(err)
	}
	g, err = l.ApplyBatch(nil)
	if err != nil || g != 1 {
		t.Fatalf("empty=(%d,%v)", g, err)
	}
	if s := l.Snapshot(); s.Generation != 1 {
		t.Fatalf("generation=%d", s.Generation)
	}
}

func TestConcurrentAccess(t *testing.T) {
	l, err := New(Options{MaxResources: 64, MaxReservations: 4096, MaxValueBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			res := fmt.Sprintf("res-%d", w)
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("id-%d-%d", w, i)
				if _, err := l.Apply(add(id, res, int64(i*2), int64(i*2+1), []byte("v"))); err != nil {
					t.Error(err)
					return
				}
				if _, err := l.At(res, int64(i*2)); err != nil {
					t.Error(err)
					return
				}
				if _, err := l.Scan(res, 0, int64(i*2+2), 1000); err != nil {
					t.Error(err)
					return
				}
				_ = l.Snapshot()
				if _, err := l.Apply(Change{Type: ChangeDelete, ID: id}); err != nil {
					t.Error(err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	s := l.Snapshot()
	if s.UsedReservations != 0 || s.Generation != 8*200*2 {
		t.Fatalf("snapshot=%+v", s)
	}
}
