package metacatalog421

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
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Kind: Put, Name: ""}}},
		{Ops: []Op{{Kind: Put, Name: "abcd", Value: []byte("v")}}},
		{Ops: []Op{{Kind: Put, Name: "A", Value: []byte("v")}}},
		{Ops: []Op{{Kind: Put, Name: "a b", Value: []byte("v")}}},
		{Ops: []Op{{Kind: Put, Name: "a", Value: nil}}},
		{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("toolong")}}},
		{Ops: []Op{{Kind: Delete, Name: "a", Value: []byte{}}}},
	}
	for i, b := range bad {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	good := Batch{Ops: []Op{{Kind: Put, Name: "a-b", Value: []byte("vv")}, {Kind: Delete, Name: "a_b"}}}
	if err := s.ValidateBatch(good); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Generation != 0 || len(got.Records) != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r1, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != r1.Generation || r2.Revision != r1.Revision || len(r2.Changed) != 0 {
		t.Fatalf("empty batch: %+v vs %+v", r2, r1)
	}
	if s.Snapshot().Generation != r1.Generation {
		t.Fatal("empty batch bumped generation")
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, err := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Mid-batch the store holds 3 records / 6 bytes, but ends within limits.
	r, err := s.Apply(Batch{Ops: []Op{
		{Kind: Put, Name: "a", Value: []byte("aa")}, {Kind: Put, Name: "b", Value: []byte("bb")}, {Kind: Put, Name: "c", Value: []byte("cc")},
		{Kind: Delete, Name: "a"}, {Kind: Delete, Name: "c"}, {Kind: Put, Name: "d", Value: []byte("dd")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "b" || r.Changed[1].Name != "d" {
		t.Fatalf("changed: %+v", r.Changed)
	}
	// Final state exceeds total value bytes: full rollback.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "b", Value: []byte("bbbb")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure did not roll back")
	}
	// Final state exceeds record count.
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "e", Value: []byte("e")}, {Kind: Put, Name: "f", Value: []byte("f")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("record-count failure did not roll back")
	}
}

func TestDeleteMissingRollsBackRevision(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("x")}, {Kind: Delete, Name: "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 1 || snap.Generation != 0 || len(snap.Records) != 0 {
		t.Fatalf("rollback leaked clocks: %+v", snap)
	}
}

func TestGetValidationAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("missing"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("xy")}}}); err != nil {
		t.Fatal(err)
	}
	r, ok, err := s.Get("a")
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	r.Value[0] = 'Q'
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'Q'
	again, _, _ := s.Get("a")
	if string(again.Value) != "xy" {
		t.Fatalf("returned slices alias internal state: %q", again.Value)
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
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) {
		t.Fatal("clone diverged")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}})
	if r.Generation != 2 || r.Revision != 2 {
		t.Fatalf("clone lost logical clocks: %+v", r)
	}
	if s.Stats().Records != 1 || s.Snapshot().NextRevision != 2 {
		t.Fatal("clone mutated original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, err := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k%02d", i)
			_, _ = s.Apply(Batch{Ops: []Op{{Kind: Put, Name: k, Value: []byte("v")}}})
			_, _, _ = s.Get(k)
			_ = s.Snapshot()
			_ = s.Stats()
			_, _ = s.Clone()
			_, _, _, _ = s.Preview(Batch{Ops: []Op{{Kind: Put, Name: k, Value: []byte("w")}}})
			_ = s.ValidateBatch(Batch{Ops: []Op{{Kind: Delete, Name: k}}})
		}()
	}
	wg.Wait()
	st := s.Stats()
	if st.Records != 32 || st.Generation != 32 || st.NextRevision != 33 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestConcurrentApplyLinearizable(t *testing.T) {
	s, err := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	const n = 16
	gens := make(chan uint64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Apply(Batch{Ops: []Op{{Kind: Put, Name: "a", Value: []byte("v")}}})
			if err != nil {
				t.Error(err)
				return
			}
			gens <- r.Generation
		}()
	}
	wg.Wait()
	close(gens)
	seen := map[uint64]bool{}
	for g := range gens {
		if seen[g] || g < 1 || g > n {
			t.Fatalf("duplicate or out-of-range generation %d", g)
		}
		seen[g] = true
	}
	if len(seen) != n || s.Snapshot().Generation != n {
		t.Fatalf("generations not linearizable: %d", len(seen))
	}
}
