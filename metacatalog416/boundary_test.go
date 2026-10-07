package metacatalog416

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

func TestStructuralValidation(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "Bad", []byte("v")}}},
		{Ops: []Op{{Put, "a b", []byte("v")}}},
		{Ops: []Op{{Put, "toolongnamehere", []byte("v")}}},
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},
		{Ops: []Op{{Delete, "a", []byte{0}}}},
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d validate: %v", i, err)
		}
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d apply: %v", i, err)
		}
	}
	if got := s.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Records) != 0 {
		t.Fatalf("state mutated: %+v", got)
	}
	if err := s.ValidateBatch(Batch{Ops: []Op{{Put, "ok_name-1", []byte("v")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, err)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	r, err = s.Apply(Batch{Ops: []Op{}})
	if err != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, err)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}}); err != nil {
		t.Fatal(err)
	}
	// Mid-batch the store exceeds limits, but the final state fits.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("56")}, {Delete, "a", nil}, {Delete, "b", nil}}}); err != nil {
		t.Fatal(err)
	}
	// Final state exceeds total value bytes: full rollback.
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("78")}, {Put, "e", []byte("90")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if after := s.Snapshot(); after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Records) != 1 {
		t.Fatalf("no rollback: %+v", after)
	}
	// Final state exceeds max records.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("7")}, {Put, "e", []byte("8")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestDeleteMissingAndGet(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("ghost"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	in := []byte("orig")
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", in}, {Put, "a", in}}}); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatalf("order: %+v", snap.Records)
	}
	if string(snap.Records[0].Value) != "orig" {
		t.Fatal("put value aliased")
	}
	snap.Records[0].Value[0] = 'Y'
	r, _, _ := s.Get("a")
	if string(r.Value) != "orig" {
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
	cs, ss := c.Snapshot(), s.Snapshot()
	if cs.Generation != ss.Generation || cs.NextRevision != ss.NextRevision {
		t.Fatal("clocks not preserved")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("clone mutation leaked")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 50; j++ {
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
	if st.Records != 16 || st.Generation != 800 || st.NextRevision != 801 || st.TotalValueBytes != 16 {
		t.Fatalf("%+v", st)
	}
	c, err := s.Clone()
	if err != nil || c.Stats() != st {
		t.Fatal(c, err)
	}
}

func TestConcurrentRevisionsUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 4096})
	var wg sync.WaitGroup
	seen := make(chan uint64, 200)
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				r, err := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k-%d-%d", i, j), []byte("v")}}})
				if err != nil {
					t.Error(err)
					return
				}
				seen <- r.Revision
			}
		}()
	}
	wg.Wait()
	close(seen)
	uniq := map[uint64]bool{}
	for rev := range seen {
		if uniq[rev] {
			t.Fatalf("duplicate revision %d", rev)
		}
		uniq[rev] = true
	}
	if len(uniq) != 200 {
		t.Fatal(len(uniq))
	}
}
