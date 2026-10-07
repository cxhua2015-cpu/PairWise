package metacatalog401

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
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	cases := []Batch{
		{Ops: []Op{{Put, "", []byte("v")}}},                            // empty name
		{Ops: []Op{{Put, "abcd", []byte("v")}}},                        // name too long
		{Ops: []Op{{Put, "A", []byte("v")}}},                           // uppercase
		{Ops: []Op{{Put, "a b", []byte("v")}}},                         // space
		{Ops: []Op{{Put, "a", nil}}},                                   // nil put value
		{Ops: []Op{{Put, "a", []byte("toolong")}}},                     // value too long
		{Ops: []Op{{Delete, "a", []byte("x")}}},                        // delete with value
		{Ops: []Op{{Kind(0), "a", nil}}},                               // unknown kind
		{Ops: []Op{{Kind(99), "a", nil}}},                              // unknown kind
		{Ops: []Op{{Put, "ok", []byte("v")}, {Put, "?", []byte("v")}}}, // bad second op
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	// Valid edge names and values.
	ok := Batch{Ops: []Op{{Put, "a-0", []byte("v")}, {Put, "z", []byte{}}, {Delete, "z", nil}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ok); err != nil {
		t.Fatal(err)
	}
	// Validation must not mutate state.
	if got := len(s.Snapshot().Records); got != 1 {
		t.Fatal(got)
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Mid-batch the store exceeds MaxRecords, but the final state fits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "c", nil},
	}})
	if err != nil || len(r.Changed) != 2 {
		t.Fatal(r, err)
	}
	// Final state exceeds MaxTotalValueBytes: whole batch rolls back.
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}, {Put, "c", []byte("1")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != 2 {
		t.Fatalf("no rollback: %+v", got)
	}
}

func TestRevisionGapsAndGeneration(t *testing.T) {
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 || r1.Generation != 1 || len(r1.Changed) != 1 || r1.Changed[0].Name != "b" {
		t.Fatalf("%+v", r1)
	}
	// Delete-only batch bumps generation once, allocates no revision.
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "b", nil}}})
	if r2.Generation != 2 || r2.Revision != 2 || len(r2.Changed) != 0 {
		t.Fatalf("%+v", r2)
	}
	// Empty batch changes nothing.
	r3, err := s.Apply(Batch{})
	if err != nil || r3.Generation != 0 {
		t.Fatalf("%+v %v", r3, err)
	}
	if snap := s.Snapshot(); snap.Generation != 2 || snap.NextRevision != 3 {
		t.Fatalf("%+v", snap)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatalf("%+v", snap)
	}
	snap.Records[0].Value[0] = 'X'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Generation != 1 || got.NextRevision != 2 || got.Records != 1 {
		t.Fatalf("%+v", got)
	}
	// Mutating the clone must not affect the original, and vice versa.
	_, _ = c.Apply(Batch{Ops: []Op{{Put, "a", []byte("zz")}}})
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "1" {
		t.Fatal("clone aliases original")
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if _, ok, _ := c.Get("a"); !ok {
		t.Fatal("original mutation leaked into clone")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("k%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
		}()
	}
	// Concurrent cloners.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				c, err := s.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = c.Apply(Batch{Ops: []Op{{Put, "tmp", []byte("x")}}})
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 16 || st.Generation != 16*50 {
		t.Fatalf("%+v", st)
	}
}
