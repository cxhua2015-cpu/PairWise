package resourcecatalog096

import (
	"errors"
	"fmt"
	"reflect"
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
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	good := []string{"a", "abc", "a-1", "_", "z9_", "---"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatal(n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b", "0x?"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatal("get", n, e)
		}
	}
}

func TestValueBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 2, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("ab")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("abc")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, op := range []Op{
		{Kind(0), "a", nil},
		{Kind(3), "a", nil},
		{Delete, "a", []byte("x")},
	} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(op, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xxxx")}, {Put, "b", []byte("xxxx")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("33")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndRollbackRevision(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestGetMissingAndCopyIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
	v := []byte("ab")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", v}}})
	v[0] = 'z'
	r, _, _ := s.Get("k")
	if string(r.Value) != "ab" {
		t.Fatal(r.Value)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, _, _ = s.Get("k")
	if string(r.Value) != "ab" {
		t.Fatal(r.Value)
	}
}

func TestSnapshotSortedAndFields(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 || len(snap.Records) != 2 ||
		snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("z")}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	seen := map[uint64]bool{}
	for _, r := range snap.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision", r.Revision)
		}
		seen[r.Revision] = true
		if r.Revision >= snap.NextRevision {
			t.Fatal(r.Revision, snap.NextRevision)
		}
	}
}
