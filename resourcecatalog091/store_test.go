package resourcecatalog091

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	ok := []string{"a", "abcd", "a-0_", "zzzz"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcde", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	// Late invalid op must fail the whole batch even though the first op is fine.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("v")}, {Kind: 99, Name: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, found, _ := s.Get("ok"); found {
		t.Fatal("partial batch committed")
	}
	// Delete carrying a value is invalid input.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ok", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Oversized value rejected before any state read.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("12345")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	// Intermediate state exceeds both limits but the final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 1 {
		t.Fatal(e, r)
	}
	// Final record count exceeded -> ErrCapacity with full rollback.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Put, "y", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := len(s.Snapshot().Records); got != 1 {
		t.Fatal(got)
	}
	// Final total value bytes exceeded -> ErrCapacity with rollback.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12345678")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	// Empty batch: no generation bump.
	r0, e := s.Apply(Batch{})
	if e != nil || r0.Generation != 0 {
		t.Fatal(e, r0)
	}
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 1 {
		t.Fatal(e, r1)
	}
	// Delete allocates no revision.
	r2, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "c", []byte("1")}}})
	if e != nil || r2.Revision != 3 {
		t.Fatal(e, r2)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 4 {
		t.Fatal(snap)
	}
	// Failed batch leaves generation and revision untouched.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); !reflect.DeepEqual(got, snap) {
		t.Fatal(got, snap)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"c", "a", "b"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatal(e)
		}
	}
	snap := s.Snapshot()
	want := []string{"a", "b", "c"}
	for i, r := range snap.Records {
		if r.Name != want[i] {
			t.Fatal(snap.Records)
		}
	}
	// Mutating the snapshot must not affect the store.
	snap.Records[0].Value[0] = 'X'
	r, found, e := s.Get("a")
	if e != nil || !found || string(r.Value) != "v" {
		t.Fatal(e, found, r)
	}
	// Result.Changed records are isolated copies too.
	res, _ := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("orig")}}})
	res.Changed[0].Value[0] = 'X'
	r2, _, _ := s.Get("d")
	if string(r2.Value) != "orig" {
		t.Fatal(r2)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, found, e := s.Get("nope"); e != nil || found {
		t.Fatal(e, found)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || len(snap.Records) != 0 {
		t.Fatal(snap.Generation, len(snap.Records))
	}
	// Revision allocations must be dense and unique across all Put ops.
	if snap.NextRevision != 32*50+1 {
		t.Fatal(snap.NextRevision)
	}
}

func TestChangedDeepEqualOrder(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Delete, "b", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	want := []Record{{Name: "a", Value: []byte("2"), Revision: 2}, {Name: "b"}}
	if !reflect.DeepEqual(r.Changed, want) {
		t.Fatal(r.Changed)
	}
}
