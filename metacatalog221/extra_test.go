package metacatalog221

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

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 2, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	for _, name := range []string{"", "abc", "A", "a b", "a.b", "é"} {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	for _, name := range []string{"a", "ab", "a-", "a_", "0", "z9"} {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}}); err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	// Delete of a missing key would be ErrNotFound, but the structurally
	// invalid op later in the batch must win because validation runs first.
	_, err := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolongvalue")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	// Transiently exceeds MaxRecords but ends within limits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")}, {Delete, "c", nil},
	}})
	if err != nil || len(r.Changed) != 2 {
		t.Fatal(r, err)
	}
	// Total value bytes exceeded at batch end.
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := len(s.Snapshot().Records); got != 2 {
		t.Fatal(got)
	}
}

func TestRevisionContinuityAndRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}}})
	if r.Revision != 2 || r.Changed[0].Revision != 1 || r.Changed[1].Revision != 2 {
		t.Fatal(r)
	}
	// Failed batch must not consume revisions or bump generation.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}, {Delete, "zz", nil}}})
	snap := s.Snapshot()
	if snap.NextRevision != 3 || snap.Generation != 1 {
		t.Fatal(snap)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("w")}}})
	if r.Revision != 3 {
		t.Fatal(r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	a, b := s.Stats(), c.Stats()
	if a != b {
		t.Fatal(a, b)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone shares state")
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
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				if j%5 == 0 {
					_, _ = s.Clone()
				}
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 32*20 || st.NextRevision != 32*20+1 {
		t.Fatalf("%+v", st)
	}
}
