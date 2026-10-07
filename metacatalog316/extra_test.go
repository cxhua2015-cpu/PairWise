package metacatalog316

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{}, {MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestNameAndValueBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	// Exact limits accepted.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a-0", []byte("zz")}}}); e != nil {
		t.Fatal(e)
	}
	// Too-long name, empty name, bad kind, too-long value rejected without state change.
	b := s.Snapshot()
	for _, b2 := range []Batch{
		{Ops: []Op{{Put, "abcd", nil}}},
		{Ops: []Op{{Put, "", nil}}},
		{Ops: []Op{{Kind(0), "a", nil}}},
		{Ops: []Op{{Kind(99), "a", nil}}},
		{Ops: []Op{{Put, "ok", []byte("toolongvalue")}}},
		{Ops: []Op{{Delete, "bad!", nil}}},
	} {
		if _, e = s.Apply(b2); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(b2, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Mid-batch the store holds 3 records / 6 bytes; final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")},
		{Delete, "a", nil}, {Put, "b", []byte("4")},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 {
		t.Fatal(e, r)
	}
	// Final record count exceeded -> rollback, revision unchanged.
	rev := s.Snapshot().NextRevision
	_, e = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Put, "y", []byte("1")}, {Put, "z", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || s.Snapshot().NextRevision != rev {
		t.Fatal(e)
	}
	// Final total value bytes exceeded -> rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
	// Failed batch must not bump generation.
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}})
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(e, ok)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "v" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*20 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
