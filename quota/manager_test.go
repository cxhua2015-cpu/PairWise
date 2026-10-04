package quota

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestReplaceRollbackPreservesState(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"a", "cpu", 4}, {"a", "mem", 4}, {"b", "cpu", 2}})
	if err := m.Reserve("r", []Demand{{"a", "cpu", 3}}, []byte("md")); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()

	// Unknown dimension in replacement: rollback.
	if err := m.Replace("r", []Demand{{"a", "nope", 1}}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	// Over-limit replacement: rollback.
	if err := m.Replace("r", []Demand{{"a", "cpu", 5}}, nil); !errors.Is(err, ErrExceeded) {
		t.Fatalf("err=%v", err)
	}
	// Metadata capacity failure: rollback.
	if err := m.Replace("r", []Demand{{"a", "cpu", 1}}, bytes.Repeat([]byte("x"), 17)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Replace on missing ID.
	if err := m.Replace("zz", []Demand{{"a", "cpu", 1}}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	if got := m.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("state mutated:\nbefore=%+v\ngot=%+v", before, got)
	}
	// Metadata total capacity failure (<= max per call but exceeds total budget).
	if err := m.Reserve("r2", []Demand{{"a", "mem", 1}}, bytes.Repeat([]byte("y"), 14)); err != nil {
		t.Fatal(err)
	}
	if err := m.Replace("r", []Demand{{"a", "cpu", 1}}, bytes.Repeat([]byte("x"), 4)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if err := m.Release("r2"); err != nil {
		t.Fatal(err)
	}

	// Moving capacity between dimensions of the same reservation is allowed.
	if err := m.Replace("r", []Demand{{"a", "mem", 4}}, nil); err != nil {
		t.Fatalf("move capacity: %v", err)
	}
	s := m.Snapshot()
	if s.SubjectState[0].Dimensions[0].Used != 0 || s.SubjectState[0].Dimensions[1].Used != 4 {
		t.Fatalf("usage=%+v", s.SubjectState[0])
	}
}

func TestLimitReductionBoundary(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 5}})
	_ = m.Reserve("r", []Demand{{"s", "cpu", 5}}, nil)
	// Reduction to exactly the used amount succeeds.
	if err := m.SetLimits([]Limit{{"s", "cpu", 5}}); err != nil {
		t.Fatal(err)
	}
	// Setting the same value still advances generation exactly once.
	gen := m.Snapshot().Generation
	if err := m.SetLimits([]Limit{{"s", "cpu", 5}}); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot().Generation; got != gen+1 {
		t.Fatalf("generation %d -> %d", gen, got)
	}
	// One below used fails.
	if err := m.SetLimits([]Limit{{"s", "cpu", 4}}); !errors.Is(err, ErrExceeded) {
		t.Fatalf("err=%v", err)
	}
	// Zero limit with zero usage is fine after release.
	_ = m.Release("r")
	if err := m.SetLimits([]Limit{{"s", "cpu", 0}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Reserve("r2", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestOverflowAndStructuralOrder(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", math.MaxInt64}})
	_ = m.Reserve("big", []Demand{{"s", "cpu", math.MaxInt64 - 1}}, nil)
	// usage + demand overflows int64: must be ErrExceeded, not a wrap-around.
	if err := m.Reserve("over", []Demand{{"s", "cpu", 2}}, nil); !errors.Is(err, ErrExceeded) {
		t.Fatalf("err=%v", err)
	}
	// Structural validation happens before any state lookup: an invalid
	// demand wins over a duplicate ID.
	if err := m.Reserve("big", []Demand{{"", "cpu", 1}}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if err := m.Reserve("big", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrExists) {
		t.Fatalf("err=%v", err)
	}
	// Invalid IDs and empty demand lists.
	if err := m.Reserve("", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if err := m.Reserve("ok", nil, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if err := m.SetLimits(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Duplicate (subject, dimension) in SetLimits is structural.
	if err := m.SetLimits([]Limit{{"s", "cpu", 1}, {"s", "cpu", 2}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
	// Name length limits.
	long := string(bytes.Repeat([]byte("n"), 9))
	if err := m.SetLimits([]Limit{{long, "cpu", 1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err=%v", err)
	}
}

func TestSubjectCapacityAndDelete(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 2, MaxReservations: 4, MaxMetadataBytes: 16, MaxNameBytes: 8})
	_ = m.SetLimits([]Limit{{"a", "cpu", 1}, {"b", "cpu", 1}})
	before := m.Snapshot()
	if err := m.SetLimits([]Limit{{"c", "cpu", 1}}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("err=%v", err)
	}
	if got := m.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatal("capacity failure mutated state")
	}
	if err := m.DeleteSubject("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	if err := m.DeleteSubject("a"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetLimits([]Limit{{"c", "cpu", 1}}); err != nil {
		t.Fatal(err)
	}
	if s := m.Snapshot(); s.Subjects != 2 {
		t.Fatalf("subjects=%d", s.Subjects)
	}
}

func TestSnapshotOwnershipIsolation(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 4}})
	in := []byte("data")
	_ = m.Reserve("r", []Demand{{"s", "cpu", 1}}, in)
	in[0] = 'X'

	s1 := m.Snapshot()
	s1.ReservationState[0].Metadata[0] = 'Y'
	s1.ReservationState[0].Demands[0].Amount = 999
	s1.SubjectState[0].Dimensions[0].Used = 999
	s2 := m.Snapshot()
	if !bytes.Equal(s2.ReservationState[0].Metadata, []byte("data")) {
		t.Fatalf("metadata aliased: %q", s2.ReservationState[0].Metadata)
	}
	if s2.ReservationState[0].Demands[0].Amount != 1 || s2.SubjectState[0].Dimensions[0].Used != 1 {
		t.Fatalf("snapshot aliased: %+v", s2)
	}
	// Mutating input demand slices after the call must not leak in.
	demands := []Demand{{"s", "cpu", 2}}
	_ = m.Reserve("r2", demands, nil)
	demands[0].Amount = 3
	if got := m.Snapshot().ReservationState[1].Demands[0].Amount; got != 2 {
		t.Fatalf("demand aliased: %d", got)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 8, MaxReservations: 128, MaxMetadataBytes: 1 << 16, MaxNameBytes: 16})
	_ = m.SetLimits([]Limit{{"s", "cpu", 1000}})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("job-%d", i)
			for j := 0; j < 20; j++ {
				if err := m.Reserve(id, []Demand{{"s", "cpu", 1}}, []byte("x")); err != nil {
					continue
				}
				_ = m.Replace(id, []Demand{{"s", "cpu", 2}}, []byte("y"))
				_ = m.Snapshot()
				_ = m.Release(id)
			}
		}()
	}
	wg.Wait()
	s := m.Snapshot()
	if s.Reservations != 0 || s.MetadataBytes != 0 || s.SubjectState[0].Dimensions[0].Used != 0 {
		t.Fatalf("leaked state: %+v", s)
	}
}

func TestConcurrentReserveNeverExceedsLimit(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 1, MaxReservations: 256, MaxMetadataBytes: 1 << 16, MaxNameBytes: 16})
	_ = m.SetLimits([]Limit{{"s", "cpu", 64}})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Reserve(fmt.Sprintf("r%d", i), []Demand{{"s", "cpu", 2}}, nil)
		}()
	}
	wg.Wait()
	s := m.Snapshot()
	used := s.SubjectState[0].Dimensions[0].Used
	if used > 64 || used%2 != 0 || int64(s.Reservations)*2 != used {
		t.Fatalf("inconsistent: used=%d reservations=%d", used, s.Reservations)
	}
}
