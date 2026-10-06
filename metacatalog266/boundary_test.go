package metacatalog266

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

func TestValidateBatchBoundaries(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a", Value: []byte("v")}}},
		{Ops: []Op{{Kind: 9, Name: "a", Value: []byte("v")}}},
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "Upper", []byte("v")}}},
		{Ops: []Op{{Put, "bad name", []byte("v")}}},
		{Ops: []Op{{Put, "waytoolongname", []byte("v")}}},
		{Ops: []Op{{Put, "a", nil}}},
		{Ops: []Op{{Put, "a", []byte("012345678")}}},
		{Ops: []Op{{Delete, "a", []byte("x")}}},
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	ok := Batch{Ops: []Op{{Put, "a-z_09", []byte("12345678")}, {Delete, "a-z_09", nil}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	before := s.Snapshot()
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != before.Generation || s.Snapshot().Generation != before.Generation {
		t.Fatal(r, err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds record capacity, ends within limits: must succeed.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "c", nil}}}); err != nil {
		t.Fatal(err)
	}
	// Final state exceeds total value bytes: must fail and roll back.
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xxxx")}, {Put, "b", []byte("yyyy")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	z := s.Stats()
	if z.Records != 2 || z.TotalValueBytes != 2 || z.Generation != 1 || z.NextRevision != 4 {
		t.Fatalf("rollback broken: %+v", z)
	}
}

func TestDeleteOnlyBatchNoRevision(t *testing.T) {
	s := store(t)
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	r2, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if err != nil || r2.Revision != r.Revision || r2.Generation != r.Generation+1 || len(r2.Changed) != 0 {
		t.Fatal(r, r2, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases store")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, name, []byte("v")}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 32 || z.Generation != 640 || z.NextRevision != 641 {
		t.Fatalf("%+v", z)
	}
	c, err := s.Clone()
	if err != nil || c.Stats() != z {
		t.Fatal(err, c.Stats(), z)
	}
}

func TestCloneClockAndOwnership(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, _ := s.Clone()
	r, err := c.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if err != nil || r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r, err)
	}
	rec, _, _ := c.Get("a")
	rec.Value[0] = 'z'
	orig, _, _ := s.Get("a")
	if string(orig.Value) != "v" {
		t.Fatal("clone aliases original")
	}
}
