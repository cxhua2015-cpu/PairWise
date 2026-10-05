package resourcecatalog091

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

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNameAndValueLimits(t *testing.T) {
	s := store(t)
	ok := []Op{{Put, "a-z_09", []byte("12345678")}}
	if _, e := s.Apply(Batch{Ops: ok}); e != nil {
		t.Fatal(e)
	}
	bad := [][]Op{
		{{Put, "", nil}},
		{{Put, "A", nil}},
		{{Put, "a b", nil}},
		{{Put, "abcdefghijklm", nil}},
		{{Put, "a", []byte("123456789")}},
	}
	for _, ops := range bad {
		if _, e := s.Apply(Batch{Ops: ops}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%v: %v", ops, e)
		}
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) || s.Snapshot().Generation != b.Generation || s.Snapshot().NextRevision != b.NextRevision || len(s.Snapshot().Records) != 0 {
		t.Fatal(e, s.Snapshot())
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r2, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r2.Revision != r1.Revision || r2.Generation != r1.Generation+1 {
		t.Fatal(e, r1, r2)
	}
	if len(r2.Changed) != 0 {
		t.Fatal(r2.Changed)
	}
}

func TestPutDeleteSameBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "b" || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "x" {
		t.Fatal(r.Value)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}, {Delete, k, nil}, {Put, k, []byte("z")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	var maxRev uint64
	for _, r := range snap.Records {
		if r.Revision > maxRev {
			maxRev = r.Revision
		}
	}
	if len(snap.Records) != 32 || snap.NextRevision != 32*20*3+1 || maxRev >= snap.NextRevision {
		t.Fatal(len(snap.Records), snap.NextRevision, maxRev)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
