package metacatalog231

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

func TestStructuralValidationBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	bad := []Op{
		{Put, "", []byte("v")},          // empty name
		{Put, "abcde", []byte("v")},     // name too long
		{Put, "Bad", []byte("v")},       // uppercase
		{Put, "a b", []byte("v")},       // space
		{Put, "a", []byte("toolong")},   // value too long
		{Put, "a", nil},                 // nil put value
		{Delete, "a", []byte{}},         // delete with value
		{Kind(0), "a", nil},             // unknown kind
		{Kind(99), "a", nil},            // unknown kind
	}
	for _, op := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	good := []Op{{Put, "a-_1", []byte("vv")}, {Delete, "a-_1", nil}}
	if err := s.ValidateBatch(Batch{Ops: good}); err != nil {
		t.Fatal(err)
	}
	// Validation must not mutate state.
	if got := s.Snapshot(); got.Generation != 0 || len(got.Records) != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds limits mid-batch but fits at the end.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("zz")},
		{Put, "b", []byte("zz")},
		{Delete, "a", nil},
	}})
	if err != nil || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, err)
	}
	// Final state exceeds: rollback everything.
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("zz")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision {
		t.Fatalf("clocks not rolled back: %+v vs %+v", before, got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 2, MaxTotalValueBytes: 4})
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			_, _, _ = s.Get(name)
			_ = s.Snapshot()
			_ = s.Stats()
			_, _ = s.Clone()
			_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("w")}}})
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 32 || st.NextRevision != 33 {
		t.Fatalf("%+v", st)
	}
}
