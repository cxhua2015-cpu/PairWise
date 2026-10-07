package metacatalog436

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

func TestValidateBatchBoundaries(t *testing.T) {
	s := store(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, Name: "a", Value: []byte("v")}}},
		{Ops: []Op{{Kind: 99, Name: "a", Value: []byte("v")}}},
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "Bad", []byte("v")}}},
		{Ops: []Op{{Put, "a b", []byte("v")}}},
		{Ops: []Op{{Put, "toolongname123", []byte("v")}}},
		{Ops: []Op{{Put, "a", nil}}},
		{Ops: []Op{{Put, "a", []byte("012345678")}}},
		{Ops: []Op{{Delete, "a", []byte("x")}}},
	}
	for i, b := range cases {
		if err := s.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	ok := Batch{Ops: []Op{{Put, "a-z_0", []byte("v")}, {Delete, "a-z_0", nil}}}
	if err := s.ValidateBatch(ok); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
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

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("3")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := len(s.Snapshot().Records); got != 0 {
		t.Fatal("state leaked after capacity failure", got)
	}
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("222")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if z := s.Stats(); z.Records != 0 || z.TotalValueBytes != 0 || z.Generation != 0 || z.NextRevision != 1 {
		t.Fatalf("clocks moved after failure: %+v", z)
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Delete, "ghost", nil}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	r, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}})
	if err != nil || r.Revision != 1 || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}}); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
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
		t.Fatal("clone clocks differ")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("clone mutation affected original")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	bad := []Batch{
		{Ops: []Op{{Put, "bad!", []byte("v")}}},
		{Ops: []Op{{Delete, "missing", nil}}},
		{Ops: []Op{{Put, "b", []byte("012345678")}}},
	}
	for i, b := range bad {
		_, applyErr := s.Apply(b)
		_, _, _, prevErr := s.Preview(b)
		if !errors.Is(prevErr, applyErr) || applyErr == nil {
			t.Fatalf("case %d: apply=%v preview=%v", i, applyErr, prevErr)
		}
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
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_, _, _, _ = s.Preview(Batch{Ops: []Op{{Put, k, []byte("w")}, {Delete, k, nil}}})
				_, _ = s.Clone()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 16 || z.Generation != 16*20 || z.NextRevision != 16*20+1 {
		t.Fatalf("%+v", z)
	}
	snap := s.Snapshot()
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
	if !reflect.DeepEqual(snap.Records[0].Value, []byte("v")) {
		t.Fatal(snap.Records[0])
	}
}
