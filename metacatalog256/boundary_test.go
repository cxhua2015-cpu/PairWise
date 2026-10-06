package metacatalog256

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
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	good := []string{"a", "abcd", "a-1_", "0"}
	for _, n := range good {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	bad := []string{"", "abcde", "A", "a b", "a.b", "é"}
	for _, n := range bad {
		if _, err := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
		if _, _, err := s.Get(n); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, err)
		}
	}
}

func TestValueBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 3, MaxTotalValueBytes: 64})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("123")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Kind: 99, Name: "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{Ops: []Op{}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits; final state fits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "c", nil},
		{Put, "a", []byte("1")},
		{Put, "b", []byte("2")},
	}})
	if err != nil || len(r.Changed) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	// Final state exceeds record limit: full rollback.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != 2 {
		t.Fatal("not rolled back")
	}
	// Final state exceeds total value bytes.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1111")}, {Put, "b", []byte("2")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestDeleteMissingAndRevisionContinuity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", nil}, {Put, "b", nil}}})
	if r.Revision != 2 {
		t.Fatal(r.Revision)
	}
	// Delete allocates no revision.
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r.Revision != 2 {
		t.Fatal(r.Revision)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "c", nil}}})
	if r.Revision != 3 {
		t.Fatal(r.Revision)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	a, b := s.Stats(), c.Stats()
	if a != b {
		t.Fatal(a, b)
	}
	rec, _, _ := c.Get("a")
	rec.Value[0] = 'X'
	again, _, _ := s.Get("a")
	if string(again.Value) != "v" {
		t.Fatal("clone aliases value")
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
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, nil}}})
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
