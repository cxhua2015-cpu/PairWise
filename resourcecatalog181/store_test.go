package resourcecatalog181

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if e != nil || x.Revision != 3 || x.Generation != 2 {
		t.Fatal(e, x)
	}
	r, ok, _ := s.Get("c")
	if !ok || r.Revision != 3 {
		t.Fatal(r, ok)
	}
	if snap := s.Snapshot(); snap.NextRevision != 4 {
		t.Fatal(snap)
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := store(t)
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Delete, "a", []byte("x")}}},
	} {
		if _, e := s.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(b, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	ok := []string{"a", "0", "-", "_", "abc-09_x", "abcdefghijkl"}
	for _, n := range ok {
		s := store(t) // MaxNameBytes 12
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatal(n, e)
		}
	}
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "é", "abcdefghijklm"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(n, e)
		}
	}
}

func TestValueBytesLimit(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Transient overflow within the batch is fine; only final state counts.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")}, {Put, "b", []byte("34")}, {Put, "c", []byte("56")},
		{Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); snap.Generation != b.Generation || snap.NextRevision != b.NextRevision || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
	s2, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	_, _ = s2.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if _, e := s2.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteNotFoundRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != b.Generation || snap.NextRevision != b.NextRevision {
		t.Fatal(snap)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("BAD!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal(string(r.Value))
	}
}

func TestSnapshotSorted(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Put, "a", nil}, {Put, "b", nil}}})
	recs := s.Snapshot().Records
	if len(recs) != 3 || recs[0].Name != "a" || recs[1].Name != "b" || recs[2].Name != "c" {
		t.Fatal(recs)
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
			k := fmt.Sprintf("key-%02d", i)
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
	// Revisions must be contiguous: 2 puts per batch, no gaps.
	if snap.NextRevision != 1+uint64(32*20*2) {
		t.Fatal(snap.NextRevision)
	}
}
