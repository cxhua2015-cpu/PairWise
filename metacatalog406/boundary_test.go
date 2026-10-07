package metacatalog406

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, err := s.Apply(Batch{})
	if err != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, err)
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	s := store(t)
	// Unknown kind and oversized value must fail before any state read.
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Put, "a", make([]byte, 9)}}},
		{Ops: []Op{{Put, "", []byte("v")}}},
		{Ops: []Op{{Put, "Bad", []byte("v")}}},
		{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "missing", nil}, {Put, "x?", nil}}},
	} {
		if _, err := s.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, err)
		}
	}
	if got := len(s.Snapshot().Records); got != 0 {
		t.Fatal(got)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds total bytes, final state does not.
	r, err := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("xx")},
		{Put, "b", []byte("xx")},
		{Put, "a", []byte("y")},
		{Delete, "b", nil},
	}})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
	z := s.Stats()
	if z.Records != 1 || z.TotalValueBytes != 1 || z.NextRevision != 4 {
		t.Fatalf("%+v", z)
	}
}

func TestCapacityRollbackClocks(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	before := s.Snapshot()
	if _, err := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}, {Put, "c", []byte("v")}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Records) != 1 {
		t.Fatalf("clocks not rolled back: %+v", after)
	}
}

func TestDeleteRemovesAndGetMisses(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if _, err := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Get("a"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, _, err := s.Get("bad?"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotDeepCopyIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal("snapshot aliases store")
	}
}

func TestCloneIndependenceAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	c, err := s.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) || c.Stats() != s.Stats() {
		t.Fatal("clone diverges")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Records) != 1 || len(c.Snapshot().Records) != 0 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a' + i))
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				if j%5 == 0 {
					_, _ = s.Clone()
				}
			}
		}()
	}
	wg.Wait()
	z := s.Stats()
	if z.Records != 16 || z.Generation != 16*20 || z.NextRevision != 16*20+1 {
		t.Fatalf("%+v", z)
	}
}
