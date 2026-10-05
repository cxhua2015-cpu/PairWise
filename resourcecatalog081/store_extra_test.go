package resourcecatalog081

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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	ok := []string{"a", "a-b_c", "0123456789ab", "z"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "0123456789abc"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: got %v", k, e)
		}
	}
}

func TestValueLengthLimit(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	// Intermediate state exceeds record cap, final state does not.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("v")}, {Put, "b", []byte("v")}, {Put, "c", []byte("v")}, {Delete, "a", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 || x.Generation != 1 {
		t.Fatal(e, x)
	}
	// Final state exceeds record cap.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("v")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Total value bytes cap.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("1234")}, {Put, "d", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}, {Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Delete then re-put in the same batch works.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}, {Put, "a", []byte("w")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(e, x)
	}
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "w" || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	b := s.Snapshot()
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != b.Generation || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().Generation != b.Generation {
		t.Fatal("generation changed on empty batch")
	}
}

func TestRevisionContinuityAcrossBatches(t *testing.T) {
	s := store(t)
	x1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}}})
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}, {Delete, "zz", nil}}})
	x2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}}})
	if x1.Revision != 2 || x2.Revision != 3 || s.Snapshot().NextRevision != 4 {
		t.Fatal(x1, x2, s.Snapshot())
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
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
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	for _, r := range snap.Records {
		if string(r.Value) != "w" {
			t.Fatal(r)
		}
	}
	if snap.Generation != 32*20 {
		t.Fatal(snap.Generation)
	}
}
