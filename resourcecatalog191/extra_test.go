package resourcecatalog191

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
			t.Fatal(o, e)
		}
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op would exceed nothing but
	// validation must happen before any state read/mutation.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("v")}, {Kind(99), "x", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 0 {
		t.Fatal("state mutated")
	}
	// Delete carrying a value is an extra field.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ok", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Oversized name and value.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "this-name-is-way-too-long", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("0123456789")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Intermediate state exceeds record capacity but final state does not.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Put, "d", []byte("4")},
		{Delete, "a", nil}, {Delete, "b", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 || r.Generation != 1 {
		t.Fatal(e, r)
	}
	// Final total value bytes exceeded -> rollback.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("01234567")}, {Put, "d", []byte("01234567")}, {Put, "e", []byte("x")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s.Snapshot().Generation != 1 {
		t.Fatal("generation not rolled back")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestGetIsolationAndMissing(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("BAD?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
	r, ok, _ := s.Get("a")
	if !ok || r.Revision != 1 {
		t.Fatal(r, ok)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, _, _ = s.Get("a")
	if string(r.Value) != "xy" {
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
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("w")}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
	// Revisions must be unique; every successful Put allocated exactly one.
	seen := map[uint64]bool{}
	for _, r := range snap.Records {
		if r.Revision == 0 || r.Revision >= snap.NextRevision {
			t.Fatal(r)
		}
		seen[r.Revision] = true
	}
	if snap.NextRevision != 32*40+1 {
		t.Fatal(snap.NextRevision, len(seen))
	}
}
