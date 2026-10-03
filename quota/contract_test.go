package quota

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func newManager(t *testing.T, o Options) *Manager {
	t.Helper()
	m, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func options() Options {
	return Options{MaxSubjects: 4, MaxReservations: 4, MaxMetadataBytes: 16, MaxNameBytes: 8}
}

func TestOptionsAndStructuralValidationFirst(t *testing.T) {
	if _, err := New(Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New=%v", err)
	}
	m := newManager(t, options())
	if err := m.SetLimits([]Limit{{Subject: "s", Dimension: "cpu", Amount: 2}}); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	err := m.SetLimits([]Limit{{Subject: "s", Dimension: "cpu", Amount: 1}, {Subject: "", Dimension: "x", Amount: -1}})
	if !errors.Is(err, ErrInvalidInput) || !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
	err = m.Reserve("r", []Demand{{Subject: "s", Dimension: "cpu", Amount: 1}, {Subject: "s", Dimension: "cpu", Amount: 1}}, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate=%v", err)
	}
}

func TestReserveReplaceReleaseAndUsage(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"b", "mem", 5}, {"a", "cpu", 4}, {"a", "mem", 6}})
	meta := []byte("job")
	if err := m.Reserve("r1", []Demand{{"a", "mem", 2}, {"a", "cpu", 3}}, meta); err != nil {
		t.Fatal(err)
	}
	meta[0] = 'X'
	if err := m.Replace("r1", []Demand{{"b", "mem", 4}}, []byte("new")); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if s.Reservations != 1 || s.MetadataBytes != 3 || s.ReservationState[0].Demands[0].Subject != "b" {
		t.Fatalf("snapshot=%+v", s)
	}
	if err := m.Release("r1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("r1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing=%v", err)
	}
}

func TestQuotaOverflowUnknownAndRollback(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", math.MaxInt64}})
	if err := m.Reserve("a", []Demand{{"s", "cpu", math.MaxInt64}}, nil); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	if err := m.Reserve("b", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrExceeded) {
		t.Fatalf("overflow=%v", err)
	}
	if err := m.Reserve("b", []Demand{{"s", "missing", 1}}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("failure mutated")
	}
}

func TestLimitReductionAndBatchRollback(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 5}, {"s", "mem", 5}})
	_ = m.Reserve("r", []Demand{{"s", "cpu", 3}}, nil)
	before := m.Snapshot()
	err := m.SetLimits([]Limit{{"s", "mem", 9}, {"s", "cpu", 2}})
	if !errors.Is(err, ErrExceeded) || !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatalf("err=%v", err)
	}
}

func TestFinalCapacityAndMetadataOwnership(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 1, MaxReservations: 1, MaxMetadataBytes: 3, MaxNameBytes: 8})
	_ = m.SetLimits([]Limit{{"s", "cpu", 2}})
	meta := []byte("abc")
	if err := m.Reserve("r", []Demand{{"s", "cpu", 1}}, meta); err != nil {
		t.Fatal(err)
	}
	meta[0] = 'X'
	s := m.Snapshot()
	if string(s.ReservationState[0].Metadata) != "abc" {
		t.Fatalf("metadata=%q", s.ReservationState[0].Metadata)
	}
	s.ReservationState[0].Metadata[0] = 'Y'
	if got := m.Snapshot().ReservationState[0].Metadata; !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("alias=%q", got)
	}
	if err := m.Replace("r", []Demand{{"s", "cpu", 2}}, []byte("xy")); err != nil {
		t.Fatalf("final replacement=%v", err)
	}
	if err := m.SetLimits([]Limit{{"s", "cpu", 3}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Reserve("r2", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrCapacity) {
		t.Fatalf("count=%v", err)
	}
}

func TestDeleteAndStableSnapshot(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"z", "x", 1}, {"a", "z", 2}, {"a", "a", 3}})
	_ = m.Reserve("r", []Demand{{"z", "x", 1}}, nil)
	if err := m.DeleteSubject("z"); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy=%v", err)
	}
	_ = m.Release("r")
	if err := m.DeleteSubject("z"); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if len(s.SubjectState) != 1 || s.SubjectState[0].Subject != "a" || s.SubjectState[0].Dimensions[0].Dimension != "a" {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestConcurrentReservations(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 1, MaxReservations: 64, MaxMetadataBytes: 64, MaxNameBytes: 8})
	_ = m.SetLimits([]Limit{{"s", "cpu", 64}})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('A' + i))
			if err := m.Reserve(id, []Demand{{"s", "cpu", 1}}, nil); err != nil {
				t.Errorf("reserve: %v", err)
			}
			_ = m.Snapshot()
		}()
	}
	wg.Wait()
	if s := m.Snapshot(); s.Reservations != 32 || s.SubjectState[0].Dimensions[0].Used != 32 {
		t.Fatalf("snapshot=%+v", s)
	}
}
