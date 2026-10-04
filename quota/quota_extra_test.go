package quota

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestReplaceRollbackOnFailure(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 4}, {"s", "mem", 4}})
	if err := m.Reserve("r", []Demand{{"s", "cpu", 3}}, []byte("md")); err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	// Exceeds limit: cpu 3 (old removed) + mem 4 ok, cpu 5 > 4 fails.
	if err := m.Replace("r", []Demand{{"s", "cpu", 5}, {"s", "mem", 1}}, nil); !errors.Is(err, ErrExceeded) {
		t.Fatalf("replace=%v", err)
	}
	// Unknown dimension.
	if err := m.Replace("r", []Demand{{"s", "gpu", 1}}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replace=%v", err)
	}
	// Metadata capacity exceeded.
	if err := m.Replace("r", []Demand{{"s", "cpu", 1}}, bytes.Repeat([]byte("x"), 17)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("replace=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("failed replace mutated state")
	}
	// Moving capacity between dimensions succeeds.
	if err := m.Replace("r", []Demand{{"s", "mem", 4}}, nil); err != nil {
		t.Fatalf("move=%v", err)
	}
	s := m.Snapshot()
	if s.ReservationState[0].Demands[0].Dimension != "mem" || s.MetadataBytes != 0 {
		t.Fatalf("snapshot=%+v", s)
	}
}

func TestReplaceMissingAndDuplicate(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 4}})
	if err := m.Replace("nope", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replace=%v", err)
	}
	if err := m.Reserve("r", []Demand{{"s", "cpu", 1}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Reserve("r", []Demand{{"s", "cpu", 1}}, nil); !errors.Is(err, ErrExists) {
		t.Fatalf("dup=%v", err)
	}
}

func TestLimitReductionBoundary(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 5}})
	_ = m.Reserve("r", []Demand{{"s", "cpu", 3}}, nil)
	// Reduction exactly to used amount succeeds.
	if err := m.SetLimits([]Limit{{"s", "cpu", 3}}); err != nil {
		t.Fatalf("exact=%v", err)
	}
	// Reduction below used fails and rolls back.
	before := m.Snapshot()
	if err := m.SetLimits([]Limit{{"s", "cpu", 2}}); !errors.Is(err, ErrExceeded) {
		t.Fatalf("below=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("reduction mutated state")
	}
	// Reduction to zero after release succeeds.
	_ = m.Release("r")
	if err := m.SetLimits([]Limit{{"s", "cpu", 0}}); err != nil {
		t.Fatalf("zero=%v", err)
	}
}

func TestOverflowAndBoundaryAmounts(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 4, MaxReservations: 8, MaxMetadataBytes: 16, MaxNameBytes: 8})
	_ = m.SetLimits([]Limit{{"s", "cpu", math.MaxInt64}, {"s", "mem", 10}})
	if err := m.Reserve("a", []Demand{{"s", "cpu", math.MaxInt64 - 1}, {"s", "mem", 10}}, nil); err != nil {
		t.Fatal(err)
	}
	// used+amount would overflow int64.
	if err := m.Reserve("b", []Demand{{"s", "cpu", 2}}, nil); !errors.Is(err, ErrExceeded) {
		t.Fatalf("overflow=%v", err)
	}
	// Exactly fits remaining 1.
	if err := m.Reserve("b", []Demand{{"s", "cpu", 1}}, nil); err != nil {
		t.Fatalf("exact=%v", err)
	}
	// Zero and negative demands are structurally invalid.
	if err := m.Reserve("c", []Demand{{"s", "cpu", 0}}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero=%v", err)
	}
	if err := m.Reserve("c", []Demand{{"s", "cpu", -1}}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("neg=%v", err)
	}
	// Negative limit invalid.
	if err := m.SetLimits([]Limit{{"s", "cpu", -1}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("neglim=%v", err)
	}
}

func TestMetadataCapacityRollback(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 2, MaxReservations: 4, MaxMetadataBytes: 4, MaxNameBytes: 8})
	_ = m.SetLimits([]Limit{{"s", "cpu", 10}})
	_ = m.Reserve("a", []Demand{{"s", "cpu", 1}}, []byte("ab"))
	before := m.Snapshot()
	// 2 existing + 3 new > 4 total.
	if err := m.Reserve("b", []Demand{{"s", "cpu", 1}}, []byte("cde")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("cap=%v", err)
	}
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
	// Replace shrinking metadata frees budget.
	if err := m.Replace("a", []Demand{{"s", "cpu", 1}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Reserve("b", []Demand{{"s", "cpu", 1}}, []byte("cdef")); err != nil {
		t.Fatalf("after=%v", err)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	m := newManager(t, options())
	_ = m.SetLimits([]Limit{{"s", "cpu", 10}})
	meta := []byte("orig")
	if err := m.Reserve("r", []Demand{{"s", "cpu", 1}}, meta); err != nil {
		t.Fatal(err)
	}
	meta[0] = 'X' // mutate input after store
	s1 := m.Snapshot()
	if string(s1.ReservationState[0].Metadata) != "orig" {
		t.Fatalf("input alias: %q", s1.ReservationState[0].Metadata)
	}
	// Mutating snapshot outputs must not affect internal state or other returns.
	s1.ReservationState[0].Metadata[0] = 'Y'
	s1.ReservationState[0].Demands[0].Amount = 999
	s1.SubjectState[0].Dimensions[0].Used = 999
	s2 := m.Snapshot()
	if string(s2.ReservationState[0].Metadata) != "orig" || s2.ReservationState[0].Demands[0].Amount != 1 || s2.SubjectState[0].Dimensions[0].Used != 1 {
		t.Fatalf("output alias: %+v", s2.ReservationState[0])
	}
	// Replace metadata input is also copied.
	meta2 := []byte("zz")
	if err := m.Replace("r", []Demand{{"s", "cpu", 2}}, meta2); err != nil {
		t.Fatal(err)
	}
	meta2[0] = 'Q'
	if got := m.Snapshot().ReservationState[0].Metadata; string(got) != "zz" {
		t.Fatalf("replace alias: %q", got)
	}
}

func TestGenerationAdvancesOncePerSuccess(t *testing.T) {
	m := newManager(t, options())
	if g := m.Snapshot().Generation; g != 0 {
		t.Fatalf("g=%d", g)
	}
	_ = m.SetLimits([]Limit{{"s", "cpu", 4}})
	_ = m.SetLimits([]Limit{{"s", "cpu", 4}}) // same value still advances
	_ = m.Reserve("r", []Demand{{"s", "cpu", 1}}, nil)
	_ = m.Replace("r", []Demand{{"s", "cpu", 2}}, nil)
	_ = m.Release("r")
	_ = m.DeleteSubject("s")
	if g := m.Snapshot().Generation; g != 6 {
		t.Fatalf("g=%d", g)
	}
	// Failures do not advance generation.
	_ = m.SetLimits([]Limit{{Subject: ""}})
	_ = m.Release("missing")
	_ = m.DeleteSubject("missing")
	if g := m.Snapshot().Generation; g != 6 {
		t.Fatalf("g=%d after failures", g)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 8, MaxReservations: 256, MaxMetadataBytes: 1024, MaxNameBytes: 16})
	_ = m.SetLimits([]Limit{{"s", "cpu", 1000}, {"s", "mem", 1000}})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a'+i)) + "-id"
			for j := 0; j < 20; j++ {
				_ = m.Reserve(id, []Demand{{"s", "cpu", 1}}, []byte("m"))
				_ = m.Replace(id, []Demand{{"s", "mem", 1}}, []byte("n"))
				_ = m.Snapshot()
				_ = m.Release(id)
			}
		}()
	}
	wg.Wait()
	s := m.Snapshot()
	if s.Reservations != 0 || s.MetadataBytes != 0 {
		t.Fatalf("leak: %+v", s)
	}
	for _, sub := range s.SubjectState {
		for _, d := range sub.Dimensions {
			if d.Used != 0 {
				t.Fatalf("usage leak: %+v", d)
			}
		}
	}
}

func TestConcurrentQuotaNeverExceeded(t *testing.T) {
	m := newManager(t, Options{MaxSubjects: 1, MaxReservations: 64, MaxMetadataBytes: 4096, MaxNameBytes: 16})
	_ = m.SetLimits([]Limit{{"s", "cpu", 10}})
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('A'+i)) + "x"
			if err := m.Reserve(id, []Demand{{"s", "cpu", 1}}, nil); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if succeeded != 10 {
		t.Fatalf("succeeded=%d", succeeded)
	}
	if got := m.Snapshot().SubjectState[0].Dimensions[0].Used; got != 10 {
		t.Fatalf("used=%d", got)
	}
}
