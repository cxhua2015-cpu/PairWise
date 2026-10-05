package servicecatalog

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	x, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abc-DEF_09", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("uppercase must be rejected")
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("empty name must be rejected")
	}
	long := "abcdefghijkl" // exactly 12 bytes, allowed
	if _, e := s.Apply(Batch{Ops: []Op{{Put, long, []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, long + "m", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("13-byte name must be rejected")
	}
}

func TestValueLengthBoundary(t *testing.T) {
	s := store(t) // MaxValueBytes 8, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("9-byte value must be rejected structurally")
	}
}

func TestTotalCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}, {Put, "b", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Exceeds total bytes only at the end; must roll back fully.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}, {Put, "c", make([]byte, 8)}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 2 {
		t.Fatal("state must be rolled back", got)
	}
	// Record count capacity.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "x", nil}, {Put, "y", nil}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteSemantics(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("delete with value must be rejected")
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("unknown kind must be rejected")
	}
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", nil}}})
	if e != nil || x.Generation != 1 || x.Revision != 1 || len(x.Changed) != 1 || x.Changed[0].Revision != 0 {
		t.Fatal(x, e)
	}
	if _, ok, e := s.Get("a"); e != nil || ok {
		t.Fatal("a must be deleted")
	}
}

func TestGetInvalidName(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("Bad"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(e, ok)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 4 || len(snap.Records) != 3 {
		t.Fatal(snap)
	}
	for i, want := range []string{"a", "b", "c"} {
		if snap.Records[i].Name != want {
			t.Fatal("records not sorted by name", snap.Records)
		}
	}
	snap.Records[0].Value[0] = 'X'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "1" {
		t.Fatal("snapshot must be isolated from store")
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
			k := fmt.Sprintf("svc-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal("all keys must be deleted", len(snap.Records))
	}
	if snap.Generation != 32*21 || snap.NextRevision != 32*20+1 {
		t.Fatal(snap.Generation, snap.NextRevision)
	}
}
