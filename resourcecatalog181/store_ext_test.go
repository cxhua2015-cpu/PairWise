package resourcecatalog181

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
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestDeleteExtraValue(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(k, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	for _, bad := range []string{"", "abcd", "A", "a b", "a.b", "é"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	for _, good := range []string{"a", "z-0", "a_b"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, good, nil}}}); e != nil {
			t.Fatalf("name %q: %v", good, e)
		}
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("zz"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Exceeds MaxRecords only at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Exceeds total value bytes only at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3333")}, {Put, "b", []byte("2222")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after failed batches")
	}
	// Delete-then-put within one batch fits capacity at the end.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("33")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionContinuity(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 || len(r1.Changed) != 1 || r1.Changed[0].Name != "b" || r1.Changed[0].Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	r2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r2.Revision != 3 || r2.Generation != 2 {
		t.Fatal(r2)
	}
	if snap := s.Snapshot(); snap.NextRevision != 4 || snap.Generation != 2 {
		t.Fatal(snap)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, "nope", nil}}})
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// Revisions must be unique and contiguous from 1.
	seen := make(map[uint64]bool)
	for _, r := range snap.Records {
		if r.Revision < 1 || r.Revision >= snap.NextRevision || seen[r.Revision] {
			t.Fatalf("bad revision %d next=%d", r.Revision, snap.NextRevision)
		}
		seen[r.Revision] = true
	}
}
