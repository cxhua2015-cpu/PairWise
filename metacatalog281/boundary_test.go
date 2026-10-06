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
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	bad := []Op{
		{Put, "", []byte("v")},        // empty name
		{Put, "abcd", []byte("v")},    // name too long
		{Put, "A", []byte("v")},       // uppercase
		{Put, "a b", []byte("v")},     // space
		{Put, "a", nil},               // empty value
		{Put, "a", []byte("toolong")}, // value too long
		{Delete, "a", []byte("x")},    // delete with value
		{Kind(0), "a", []byte("v")},   // unknown kind
		{Kind(99), "a", []byte("v")},  // unknown kind
	}
	for _, op := range bad {
		if e := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if e := s.ValidateBatch(Batch{Ops: []Op{{Put, "a_1", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	// ValidateBatch must not read state: delete of a missing name is fine.
	if e := s.ValidateBatch(Batch{Ops: []Op{{Delete, "g", nil}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{1, 4, 4, 4})
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently two records, but the batch ends within limits.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("w")}, {Delete, "a", nil}}})
	if e != nil || len(r.Changed) != 2 {
		t.Fatal(r, e)
	}
	if z := s.Stats(); z.Records != 1 || z.TotalValueBytes != 1 || z.NextRevision != 3 {
		t.Fatalf("%+v", z)
	}
	// Exceeding total value bytes at batch end fails and rolls back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("wwww")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if z := s.Stats(); z.Records != 1 || z.NextRevision != 3 || z.Generation != 1 {
		t.Fatalf("%+v", z)
	}
	// Exceeding record count at batch end fails too.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("x")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteSemantics(t *testing.T) {
	s, _ := New(Options{4, 8, 4, 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	// Delete does not allocate a revision.
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Revision != 1 || s.Stats().NextRevision != 2 {
		t.Fatal(r, e)
	}
	if _, ok, e := s.Get("a"); e != nil || ok {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s, _ := New(Options{8, 8, 8, 64})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	c, e := s.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if s.Stats() != c.Stats() {
		t.Fatal("clocks diverged")
	}
	// Mutating the clone must not leak into the original.
	if _, e := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("w")}}}); e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 1 || len(c.Snapshot().Records) != 1 {
		t.Fatal("state leaked")
	}
	// Mutating a value obtained from the original must not affect the clone.
	r, _, _ := s.Get("a")
	r.Value[0] = 'X'
	rc, _, _ := c.Get("b")
	if string(rc.Value) != "w" {
		t.Fatal("value aliased")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{128, 12, 8, 4096})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 16 || z.Generation != 800 || z.NextRevision != 801 {
		t.Fatalf("%+v", z)
	}
	c, e := s.Clone()
	if e != nil || c.Stats() != z {
		t.Fatal(e)
	}
}

func TestConcurrentRevisionsUnique(t *testing.T) {
	s, _ := New(Options{64, 12, 8, 4096})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 25; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("r%d", i), []byte("v")}}})
			}
		}()
	}
	w.Wait()
	seen := map[uint64]bool{}
	for _, r := range s.Snapshot().Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[r.Revision] = true
	}
	if z := s.Stats(); z.NextRevision != 201 {
		t.Fatalf("%+v", z)
	}
}
