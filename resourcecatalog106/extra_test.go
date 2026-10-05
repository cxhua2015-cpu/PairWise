package resourcecatalog106

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -1}, {},
	} {
		if _, e := New(Options{o.MaxRecords, o.MaxNameBytes, o.MaxValueBytes, o.MaxTotalValueBytes}); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	good := []string{"a", "abc", "a-1_", "0", "z9"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcde", "A", "a B", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("none"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r.Generation != 2 || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(r)
	}
	if snap := s.Snapshot(); snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", nil}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1111")}, {Put, "b", []byte("2")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation must not change on failed batch")
	}
	// Overwrite existing key with larger value exceeding total budget mid-batch.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("3333")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	r, _, _ := s.Get("a")
	if string(r.Value) != "11" {
		t.Fatal(string(r.Value))
	}
}

func TestDeleteMissingAndDoubleDelete(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", nil}}})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("rollback failed")
	}
	// Put then Delete same key in one batch: net effect is absence.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("v")}, {Delete, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("expected absence")
	}
}

func TestChangedSortedAndSnapshotIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	if len(r.Changed) != 3 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" || r.Changed[2].Name != "c" {
		t.Fatal(r.Changed)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r2, _, _ := s.Get("a")
	if string(r2.Value) != "2" {
		t.Fatal("snapshot not isolated")
	}
	r.Changed[0].Value[0] = 'Y'
	r3, _, _ := s.Get("a")
	if string(r3.Value) != "2" {
		t.Fatal("result not isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || snap.NextRevision < 1 {
		t.Fatal(snap)
	}
	for _, r := range snap.Records {
		if r.Revision == 0 || r.Revision >= snap.NextRevision {
			t.Fatal(r)
		}
	}
}
