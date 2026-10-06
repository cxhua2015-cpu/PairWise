package metacatalog251

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
	bad := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Put, "", nil},
		{Put, "Bad", nil},
		{Put, "a b", nil},
		{Put, "a/b", nil},
		{Put, "toolongname123", nil},
		{Put, "a", make([]byte, 9)},
		{Delete, "a", []byte{}},
		{Delete, "a", []byte("x")},
	}
	for _, op := range bad {
		if e := s.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	good := []Op{
		{Put, "a", nil},
		{Put, "z-9_", []byte("12345678")},
		{Delete, "a", nil},
	}
	for _, op := range good {
		if e := s.ValidateBatch(Batch{Ops: []Op{op}}); e != nil {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed")
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "a", nil}, {Delete, "b", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("1234")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	sn := s.Snapshot()
	if sn.Generation != b.Generation || sn.NextRevision != b.NextRevision || len(sn.Records) != len(b.Records) {
		t.Fatal("not rolled back")
	}
}

func TestRevisionRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "missing", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	sn := s.Snapshot()
	if sn.NextRevision != 2 || sn.Generation != 1 || len(sn.Records) != 1 {
		t.Fatalf("%+v", sn)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	sn := s.Snapshot()
	sn.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal("snapshot aliases store")
	}
	c, _ := s.Clone()
	cr, _, _ := c.Get("a")
	cr.Value[0] = 'W'
	r, _, _ = s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal("clone aliases store")
	}
	if c.Snapshot().Generation != 1 || c.Stats().NextRevision != 2 {
		t.Fatal("clone lost logical clocks")
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
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, nil}}})
				if j%5 == 0 {
					_, _ = s.Clone()
				}
			}
		}()
	}
	w.Wait()
	st := s.Stats()
	if st.Records != 16 || st.Generation != 320 || st.NextRevision != 321 {
		t.Fatalf("%+v", st)
	}
}
