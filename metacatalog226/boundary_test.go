package metacatalog226

import (
	"errors"
	"fmt"
	"reflect"
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
	s := store(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},                      // unknown kind
		{Ops: []Op{{Kind: 99, Name: "a"}}},                     // unknown kind
		{Ops: []Op{{Put, "", []byte("v")}}},                    // empty name
		{Ops: []Op{{Put, "Upper", []byte("v")}}},               // uppercase
		{Ops: []Op{{Put, "a b", []byte("v")}}},                 // space
		{Ops: []Op{{Put, "é", []byte("v")}}},                   // non-ASCII
		{Ops: []Op{{Put, "abcdefghijklm", []byte("v")}}},       // name too long
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},               // value too long
		{Ops: []Op{{Delete, "a", []byte("x")}}},                // delete with value
		{Ops: []Op{{Put, "ok", []byte("v")}, {Put, "?", nil}}}, // fail late in batch
	}
	for i, b := range bad {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	good := Batch{Ops: []Op{
		{Put, "a-z_0-9", []byte("12345678")},
		{Delete, "a-z_0-9", nil},
	}}
	if err := s.ValidateBatch(good); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(good); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityRollbackKeepsClocks(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	r1, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}})
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// Exceeds total value bytes only at batch end.
	_, err = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("x")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Exceeds record count only at batch end.
	_, err = s.Apply(Batch{Ops: []Op{
		{Put, "b", nil}, {Put, "c", nil}, {Put, "d", nil},
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("state changed: %+v vs %+v", before, after)
	}
	// Clocks unchanged: next successful batch continues the sequence.
	r2, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}}})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != r1.Generation+1 || r2.Revision != r1.Revision+1 {
		t.Fatalf("clocks: %+v -> %+v", r1, r2)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}}); err != nil {
		t.Fatal(err)
	}
	r, ok, err := s.Get("a")
	if err != nil || !ok || string(r.Value) != "2" || r.Revision != 2 {
		t.Fatal(r, ok, err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad!"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("nope"); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatalf("order: %+v", snap.Records)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
	if snap.Generation != 1 || snap.NextRevision != 4 {
		t.Fatalf("clocks: %+v", snap)
	}
}

func TestStatsLinearizable(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("345")}}}); err != nil {
		t.Fatal(err)
	}
	st := s.Stats()
	if st.Generation != 1 || st.NextRevision != 3 || st.Records != 2 || st.TotalValueBytes != 5 {
		t.Fatalf("%+v", st)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	st = s.Stats()
	if st.Generation != 2 || st.NextRevision != 3 || st.Records != 1 || st.TotalValueBytes != 3 {
		t.Fatalf("%+v", st)
	}
}

func TestCloneIndependence(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clone diverges at birth")
	}
	// Mutate clone: original must not move.
	if _, err := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("z")}, {Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Records != 1 || c.Stats().Records != 1 || s.Stats().Generation != 1 || c.Stats().Generation != 2 {
		t.Fatal("clone writes leaked")
	}
	// Mutate original's stored value via clone snapshot aliasing check.
	snap := c.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, _, _ := c.Get("b")
	if string(r.Value) != "z" {
		t.Fatal("clone snapshot aliases")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
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
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 640 || st.NextRevision != 641 {
		t.Fatalf("%+v", st)
	}
	c, err := s.Clone()
	if err != nil || c.Stats() != st {
		t.Fatal(c.Stats(), err)
	}
}
