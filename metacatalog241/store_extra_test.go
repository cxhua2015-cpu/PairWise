package metacatalog241

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", nil},
		{Put, "Upper", nil},
		{Put, "has space", nil},
		{Put, "toolongname123", nil},
		{Put, "a", make([]byte, 9)},
		{Delete, "a", []byte{}},
		{Delete, "a", []byte("x")},
		{Kind(0), "a", nil},
		{Kind(99), "a", nil},
	}
	for _, op := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "ok-1_2", nil}, {Delete, "ok-1_2", nil}}}); err != nil {
		t.Fatal(err)
	}
	// ValidateBatch must not mutate state.
	if snap := s.Snapshot(); snap.Generation != 0 || len(snap.Records) != 0 {
		t.Fatalf("validate mutated state: %+v", snap)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Transiently exceeds MaxRecords mid-batch, fine at the end.
	_, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "c", nil}}})
	if err != nil {
		t.Fatal(err)
	}
	// Exceeds total bytes only at the end.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}, {Put, "c", []byte("1")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 2 || z.TotalValueBytes != 2 || z.Generation != 1 {
		t.Fatalf("rollback failed: %+v", z)
	}
}

func TestDeleteMissingAndRevisionContinuity(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", nil}, {Put, "b", nil}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
	// Delete allocates no revision.
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "c", nil}}})
	if r.Revision != 3 {
		t.Fatal(r)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependence(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clone clocks differ")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone not independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k-%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("w")}, {Delete, name, nil}}})
			_, _, _ = s.Get(name)
			_ = s.Snapshot()
			_ = s.Stats()
			_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, nil}}})
			_, _ = s.Clone()
		}()
	}
	wg.Wait()
	z := s.Stats()
	if z.Records != 0 || z.Generation != 64 || z.NextRevision != 65 {
		t.Fatalf("%+v", z)
	}
}
