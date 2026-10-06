package metacatalog236

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
	s := store(t)
	for _, name := range []string{"", "A", "a b", "a.b", "é", "toolongname123"} {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, name, nil}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	for _, name := range []string{"a", "0", "-", "_", "abc-def_09", "exactly12chr"} {
		if err := s.ValidateBatch(Batch{Ops: []Op{{Put, name, nil}}}); err != nil {
			t.Fatalf("name %q: %v", name, err)
		}
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown kind accepted")
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("oversize value accepted")
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Apply(Batch{})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits; final state fits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("ab")}, {Put, "b", []byte("cd")}, {Put, "c", []byte("ef")},
		{Delete, "a", nil}, {Delete, "c", nil},
	}})
	if err != nil || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatalf("%+v %v", r, err)
	}
	z := s.Stats()
	if z.Records != 1 || z.TotalValueBytes != 2 || z.Generation != 1 || z.NextRevision != 4 {
		t.Fatalf("%+v", z)
	}
}

func TestCapacityRollbackClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("wxyz")}}}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("q")}, {Put, "a", []byte("wxyz")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision || len(after.Records) != 1 {
		t.Fatalf("clocks advanced: %+v -> %+v", before, after)
	}
}

func TestDeleteMissingAndValueRejected(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("ghost"); ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clocks diverge")
	}
	r, _, _ := c.Get("a")
	r.Value[0] = 'z'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "x" {
		t.Fatal("clone aliases value")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, nil}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 32 || z.Generation != 640 || z.NextRevision != 641 {
		t.Fatalf("%+v", z)
	}
}
