package metacatalog216

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

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{Ops: nil})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	ok := []string{"a", "abc-xyz", "012345678901", "with-dash", "with_under"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "Bad", "has space", "has/slash", "has.dot", "0123456789012", "é", "UPPER"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, _, e := s.Get("Bad"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionOnlyOnPut(t *testing.T) {
	s := store(t)
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r.Revision != 2 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 3 || snap.Generation != 2 {
		t.Fatal(snap)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Transiently exceeds both limits, final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "c" {
		t.Fatal(r, e)
	}
	// Final record count exceeded -> rollback.
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("7")}, {Put, "e", []byte("8")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Final total value bytes exceeded -> rollback.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1234")}, {Put, "d", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestRollbackRestoresRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 2 || snap.Generation != 1 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
}

func TestDeleteMissingAndChangedExcludesDeleted(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
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
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 32*51 {
		t.Fatal(snap.Generation, len(snap.Records))
	}
}
