package reservation

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func newLedger(t *testing.T) *Ledger {
	t.Helper()
	l, e := New(Options{MaxResources: 4, MaxReservations: 16, MaxValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func add(id, res string, start, end int64, v []byte) Change {
	return Change{Type: ChangeAdd, Reservation: Reservation{ID: id, Resource: res, Start: start, End: end, Value: v}}
}

func TestOptionsAndValidation(t *testing.T) {
	for _, o := range []Options{{}, {0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {10001, 1, 1}, {1, 1000001, 1}, {1, 1, 64<<20 + 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("New(%+v)=%v", o, e)
		}
	}
	l := newLedger(t)
	tests := []struct {
		c    Change
		want error
	}{{Change{Type: 99}, ErrInvalidChange}, {Change{Type: ChangeAdd, ID: "x", Reservation: Reservation{ID: "a", Resource: "r", Start: 1, End: 2}}, ErrInvalidChange}, {add("bad/id", "r", 1, 2, nil), ErrInvalidID}, {add("a", "bad/r", 1, 2, nil), ErrInvalidResource}, {add("a", "r", 2, 2, nil), ErrInvalidInterval}, {add("a", "r", 1, 2, make([]byte, 1<<20+1)), ErrValueTooLarge}, {Change{Type: ChangeDelete, ID: "a", Reservation: Reservation{ID: "x"}}, ErrInvalidChange}}
	for _, x := range tests {
		if _, e := l.Apply(x.c); !errors.Is(e, x.want) {
			t.Errorf("Apply=%v want %v", e, x.want)
		}
	}
}

func TestBatchFinalConflictAndRollback(t *testing.T) {
	l := newLedger(t)
	g, e := l.ApplyBatch([]Change{add("a", "r", 0, 10, nil), add("b", "r", 10, 20, nil)})
	if e != nil || g != 1 {
		t.Fatalf("batch=(%d,%v)", g, e)
	}
	before := l.Snapshot()
	_, e = l.ApplyBatch([]Change{{Type: ChangeDelete, ID: "a"}, add("c", "r", 5, 15, nil), add("d", "r", 14, 16, nil)})
	if !errors.Is(e, ErrConflict) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatalf("conflict=%v changed", e)
	}
	_, e = l.ApplyBatch([]Change{add("a", "x", 1, 2, nil), add("bad/id", "x", 2, 3, nil)})
	if !errors.Is(e, ErrInvalidID) {
		t.Fatalf("validation order=%v", e)
	}
}

func TestDeleteReaddAndFinalCapacity(t *testing.T) {
	l, _ := New(Options{MaxResources: 1, MaxReservations: 1, MaxValueBytes: 2})
	v := []byte("aa")
	_, e := l.Apply(add("a", "r", 1, 2, v))
	if e != nil {
		t.Fatal(e)
	}
	v[0] = 'X'
	g, e := l.ApplyBatch([]Change{{Type: ChangeDelete, ID: "a"}, add("a", "r", 2, 3, []byte("bb"))})
	if e != nil || g != 2 {
		t.Fatalf("replace=(%d,%v)", g, e)
	}
	before := l.Snapshot()
	_, e = l.Apply(add("b", "x", 3, 4, nil))
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, l.Snapshot()) {
		t.Fatalf("capacity=%v", e)
	}
}

func TestAtHalfOpenAndIsolation(t *testing.T) {
	l := newLedger(t)
	_, _ = l.ApplyBatch([]Change{add("min", "r", math.MinInt64, -1, []byte("m")), add("max", "r", 0, math.MaxInt64, []byte("x"))})
	for _, tc := range []struct {
		at    int64
		found bool
		id    string
	}{{math.MinInt64, true, "min"}, {-1, false, ""}, {0, true, "max"}, {math.MaxInt64 - 1, true, "max"}, {math.MaxInt64, false, ""}} {
		r, e := l.At("r", tc.at)
		if e != nil || r.Found != tc.found || r.Reservation.ID != tc.id {
			t.Errorf("At(%d)=%+v %v", tc.at, r, e)
		}
	}
	r, _ := l.At("r", 0)
	r.Reservation.Value[0] = 'Y'
	if string(l.Snapshot().Resources[0].Reservations[1].Value) != "x" {
		t.Fatal("At aliases state")
	}
}

func TestScanAndSnapshotOrdering(t *testing.T) {
	l := newLedger(t)
	_, _ = l.ApplyBatch([]Change{add("z", "b", 10, 20, nil), add("c", "a", 20, 30, nil), add("b", "a", 10, 20, nil)})
	r, e := l.Scan("a", 15, 25, 10)
	if e != nil || len(r.Reservations) != 2 || r.Reservations[0].ID != "b" || r.Reservations[1].ID != "c" {
		t.Fatalf("scan=%+v %v", r, e)
	}
	s := l.Snapshot()
	if len(s.Resources) != 2 || s.Resources[0].Resource != "a" || s.Resources[1].Resource != "b" {
		t.Fatalf("snapshot=%+v", s)
	}
	if _, e = l.Scan("a", 1, 1, 1); !errors.Is(e, ErrInvalidScan) {
		t.Fatal(e)
	}
	if _, e = l.Scan("a", 1, 2, 0); !errors.Is(e, ErrInvalidScan) {
		t.Fatal(e)
	}
}
