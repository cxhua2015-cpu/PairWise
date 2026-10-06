package metacatalog281

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

func TestNameAndValueBoundaries(t *testing.T) {
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	bad := []Op{
		{Put, "", nil},
		{Put, "abcd", nil},
		{Put, "A", nil},
		{Put, "a b", nil},
		{Put, "a", []byte("xyz")},
		{Delete, "a", []byte{}},
		{Kind(0), "a", nil},
		{Kind(3), "a", nil},
	}
	for _, op := range bad {
		if err := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	good := Batch{Ops: []Op{{Put, "a_0", []byte("ok")}}}
	if err := s.ValidateBatch(good); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(good); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityRollbackPreservesClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34")}, {Put, "c", []byte("5")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision || len(after.Records) != 1 {
		t.Fatalf("clocks/state changed: %+v -> %+v", before, after)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
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

func TestDeleteMissingAndGetInvalid(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.Get("Bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k-%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
			_ = s.Stats()
			if i%4 == 0 {
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
			if i%8 == 0 {
				_, _ = s.Clone()
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	snap := s.Snapshot()
	if st.Records != len(snap.Records) || st.Generation != snap.Generation || st.NextRevision != snap.NextRevision {
		t.Fatalf("inconsistent: %+v vs %+v", st, snap)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
