package metacatalog246

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

func TestStructuralValidation(t *testing.T) {
	s := store(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "Bad", []byte("v")}}},
		{Ops: []Op{{Put, "a b", []byte("v")}}},
		{Ops: []Op{{Put, "a/b", []byte("v")}}},
		{Ops: []Op{{Put, "this-name-is-too-long", []byte("v")}}},
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},
		{Ops: []Op{{Delete, "a", []byte{0}}}},
	}
	for i, b := range bad {
		if e := s.ValidateBatch(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
		if _, e := s.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, e)
		}
	}
	good := Batch{Ops: []Op{{Put, "a-0_z", []byte("ok")}}}
	if e := s.ValidateBatch(good); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Transiently exceeds record limit mid-batch, fine at the end.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")}, {Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Total value bytes exceeded at end -> rollback.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4444")}, {Put, "e", []byte("5555")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != before.Generation || got.NextRevision != before.NextRevision || len(got.Records) != len(before.Records) {
		t.Fatal("no rollback")
	}
	// Record count exceeded at end -> rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Put, "e", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "missing", nil}}})
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("3")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	rec, ok, _ := s.Get("a")
	if !ok || string(rec.Value) != "2" || rec.Revision != 2 {
		t.Fatal(rec, ok)
	}
	if _, ok, _ := s.Get("zz"); ok {
		t.Fatal("unexpected hit")
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "x" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneClocksAndIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}}})
	c, e := s.Clone()
	if e != nil {
		t.Fatal(e)
	}
	cs, ss := c.Snapshot(), s.Snapshot()
	if cs.Generation != ss.Generation || cs.NextRevision != ss.NextRevision {
		t.Fatal("clocks not preserved")
	}
	// Clone continues revision sequence independently.
	r, _ := c.Apply(Batch{Ops: []Op{{Put, "c", []byte("z")}}})
	if r.Revision != 3 {
		t.Fatal(r)
	}
	if len(s.Snapshot().Records) != 2 {
		t.Fatal("clone mutated original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for n := 0; n < 20; n++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	st := s.Stats()
	if st.Records != 0 || st.TotalValueBytes != 0 {
		t.Fatal(st)
	}
	if st.Generation != 16*20+16 {
		t.Fatal(st)
	}
	if st.NextRevision != 16*20+1 {
		t.Fatal(st)
	}
}
