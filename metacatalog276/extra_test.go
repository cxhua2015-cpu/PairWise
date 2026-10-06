package metacatalog276

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsMustBePositive(t *testing.T) {
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
	cases := []Batch{
		{Ops: []Op{{Put, "", []byte("v")}}},                      // empty name
		{Ops: []Op{{Put, "Upper", []byte("v")}}},                 // uppercase
		{Ops: []Op{{Put, "name?", []byte("v")}}},                 // bad char
		{Ops: []Op{{Put, "this-name-is-too-long", []byte("v")}}}, // > MaxNameBytes
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},                 // > MaxValueBytes
		{Ops: []Op{{Kind: 0, Name: "a"}}},                        // unknown kind
		{Ops: []Op{{Kind: 99, Name: "a"}}},                       // unknown kind
		{Ops: []Op{{Delete, "a", []byte{}}}},                     // delete with value
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	ok := Batch{Ops: []Op{{Put, "ok_name-1", make([]byte, 8)}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	r, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
	r, err = s.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestDeleteDoesNotAllocateRevision(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if err != nil || r.Revision != 1 {
		t.Fatal(r, err)
	}
	r, err = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}})
	if err != nil || r.Revision != 2 {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds record capacity, fine at batch end.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "c", nil},
	}})
	if err != nil || len(r.Changed) != 3 {
		t.Fatal(r, err)
	}
	// Final state exceeds total value bytes: full rollback.
	before := s.Snapshot()
	_, err = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xxxx")}, {Put, "b", []byte("yyyy")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != len(before.Records) {
		t.Fatal("state changed after failed batch")
	}
	// Exceeding MaxRecords at batch end.
	_, err = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("z")}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "xy" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != s.Stats() {
		t.Fatal("clocks diverge")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("z")}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatal(r)
	}
	if s.Stats().Generation != 1 || s.Stats().Records != 1 {
		t.Fatal("clone mutated original")
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
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 640 || st.NextRevision != 641 {
		t.Fatalf("%+v", st)
	}
}
