package metacatalog276

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, err := New(valid); err != nil {
		t.Fatal(err)
	}
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -1}, {},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralBoundaries(t *testing.T) {
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	ok := Batch{Ops: []Op{{Put, "a-1", []byte("12")}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
	bad := []Batch{
		{Ops: []Op{{Put, "", nil}}},                    // empty name
		{Ops: []Op{{Put, "abcd", nil}}},                // name too long
		{Ops: []Op{{Put, "A", nil}}},                   // uppercase
		{Ops: []Op{{Put, "a b", nil}}},                 // space
		{Ops: []Op{{Put, "a", []byte("123")}}},         // value too long
		{Ops: []Op{{Kind(0), "a", nil}}},               // unknown kind
		{Ops: []Op{{Kind(3), "a", nil}}},               // unknown kind
		{Ops: []Op{{Delete, "a", []byte{}}}},           // delete with value
		{Ops: []Op{{Delete, "a", []byte("x")}}},        // delete with value
		{Ops: []Op{{Put, "ok", nil}, {Put, "?", nil}}}, // later op invalid
	}
	for i, b := range bad {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if got := s.Stats(); got.Records != 0 || got.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Mid-batch the store holds 3 records / 6 bytes, over both limits;
	// only the final state counts.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
	}})
	if err != nil || len(r.Changed) != 3 {
		t.Fatal(r, err)
	}
	if z := s.Stats(); z.Records != 2 || z.TotalValueBytes != 4 {
		t.Fatalf("%+v", z)
	}
	// Exceeding final capacity fails and rolls back clocks.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestDeleteMissingRollsBackRevision(t *testing.T) {
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "zz", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	z := s.Stats()
	if z.Generation != 0 || z.NextRevision != 1 || z.Records != 0 {
		t.Fatalf("%+v", z)
	}
}

func TestChangedSortedAndDeepCopied(t *testing.T) {
	s, err := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Delete, "b", nil},
		{Put, "c", []byte("3")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	if len(r.Changed) != len(want) {
		t.Fatal(r.Changed)
	}
	for i, rec := range r.Changed {
		if rec.Name != want[i] {
			t.Fatalf("changed[%d]=%s", i, rec.Name)
		}
	}
	// Mutating returned Changed values must not affect the store.
	r.Changed[0].Value[0] = 'x'
	got, _, _ := s.Get("a")
	if string(got.Value) != "2" {
		t.Fatal("Changed aliases store")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'x'
	got, _, _ := s.Get("a")
	if string(got.Value) != "v" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneSharesClocksNotMemory(t *testing.T) {
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.Stats(), s.Stats(); got != want {
		t.Fatalf("clone clocks %+v != %+v", got, want)
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone aliases original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 32, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 128})
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
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, nil}}})
			}
		}()
	}
	wg.Wait()
	z := s.Stats()
	if z.Records != 16 || z.NextRevision != 16*20+1 || z.Generation != 16*20 {
		t.Fatalf("%+v", z)
	}
}
