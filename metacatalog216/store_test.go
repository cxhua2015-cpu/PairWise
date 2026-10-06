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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	// Empty, too long, uppercase, non-ASCII, and punctuation are rejected.
	for _, n := range []string{"", "abcd", "Ab", "a?", "a b", "é", "a.b"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	// Boundary-valid names are accepted.
	for _, n := range []string{"a", "z09", "-_", "a-b"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueLengthBoundary(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("ab")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("abc")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", nil}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	b := s.Snapshot()
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: got %v", k, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid kind")
	}
}

func TestEmptyBatch(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestDeleteMissingAndNotFound(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("ghost"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Exceeds MaxRecords at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", nil}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Exceeds MaxTotalValueBytes at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("3333")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); !reflect.DeepEqual(b, got) {
		t.Fatalf("rollback mismatch: %+v vs %+v", b, got)
	}
}

func TestRevisionContinuityAndGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	// Delete allocates no revision.
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Generation != 2 || x.Revision != 2 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Revision != 3 {
		t.Fatal(x)
	}
	// Failed batch does not consume revisions or generation.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Delete, "nope", nil}}})
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if x.Generation != 4 || x.Revision != 4 {
		t.Fatal(x)
	}
	snap := s.Snapshot()
	if snap.Generation != 4 || snap.NextRevision != 5 {
		t.Fatal(snap)
	}
}

func TestChangedDeduplicatedSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// "a" was deleted by batch end, so only "c" remains in Changed.
	if len(x.Changed) != 1 || x.Changed[0].Name != "c" || x.Changed[0].Revision != 3 || string(x.Changed[0].Value) != "3" {
		t.Fatal(x.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	// Mutating the snapshot must not affect the store.
	snap.Records[0].Value[0] = 'z'
	snap.Records[1].Name = "zz"
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "2" {
		t.Fatal(r, ok)
	}
	if _, ok, _ := s.Get("b"); !ok {
		t.Fatal("store mutated via snapshot")
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation != 32*20*2 {
		t.Fatalf("generation=%d", snap.Generation)
	}
	if len(snap.Records) != 0 {
		t.Fatalf("records=%d", len(snap.Records))
	}
	// Revisions are continuous: 1 put per iteration per goroutine.
	if snap.NextRevision != 1+uint64(32*20) {
		t.Fatalf("nextRevision=%d", snap.NextRevision)
	}
}
