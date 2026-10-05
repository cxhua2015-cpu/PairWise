package resourcecatalog156

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if e != nil || x.Generation != 1 || x.Revision != 2 {
		t.Fatal(e, x)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(x.Changed)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 3 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Exceeds MaxRecords mid-batch but ends within limits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")},
		{Delete, "a", nil},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	// Ends over MaxRecords: must fail and roll back.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Put, "e", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Ends over total value bytes: must fail and roll back.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "b", nil}, {Put, "d", []byte("4444")}, {Put, "e", []byte("5555")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestOverwriteAccounting(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}}}); e != nil {
		t.Fatal(e)
	}
	// Overwrite frees old bytes; total stays within limit.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "12345678" || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestGetNotFoundAndOwnership(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
	v := []byte("orig")
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", v}}}); e != nil {
		t.Fatal(e)
	}
	v[0] = 'X' // caller mutation must not leak into the store
	r, _, _ := s.Get("a")
	if string(r.Value) != "orig" {
		t.Fatal(r)
	}
	r.Value[0] = 'Y' // mutating returned value must not leak either
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "orig" {
		t.Fatal(r2)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Z'
	r3, _, _ := s.Get("a")
	if string(r3.Value) != "orig" {
		t.Fatal(r3)
	}
}

func TestSnapshotSorted(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}}); e != nil {
		t.Fatal(e)
	}
	recs := s.Snapshot().Records
	if len(recs) != 3 || recs[0].Name != "a" || recs[1].Name != "b" || recs[2].Name != "c" {
		t.Fatal(recs)
	}
}

func TestDeleteNotFoundRollbackRevision(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 2 || snap.Generation != 1 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 16, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%03d", i%16)
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
	// Revisions must be strictly increasing and unique across all records.
	seen := map[uint64]bool{}
	for _, r := range snap.Records {
		if r.Revision == 0 || r.Revision >= snap.NextRevision || seen[r.Revision] {
			t.Fatal(r, snap.NextRevision)
		}
		seen[r.Revision] = true
	}
}
