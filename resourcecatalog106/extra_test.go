package resourcecatalog106

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
		{}, {MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatal(o, e)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op would hit ErrNotFound
	// if state were read before full validation.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "BAD!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Unknown kind and Delete carrying a value are invalid.
	for _, ops := range [][]Op{
		{{Kind: 0, Name: "a"}},
		{{Kind: 99, Name: "a"}},
		{{Delete, "a", []byte("x")}},
		{{Put, "", []byte("x")}},
		{{Put, "Upper", []byte("x")}},
		{{Put, "has space", []byte("x")}},
		{{Put, "way-too-long-name", []byte("x")}},
		{{Put, "ok", make([]byte, 9)}},
	} {
		if _, e := s.Apply(Batch{Ops: ops}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(ops, e)
		}
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Mid-batch there are 4 records / 4 bytes over nothing, but the batch
	// ends within limits, so it must succeed.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")},
		{Put, "c", []byte("1234")}, {Put, "d", []byte("1234")},
		{Delete, "d", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Ending over the record limit fails and rolls back.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "c", nil}, {Put, "x", []byte("1")}, {Put, "y", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Ending over total value bytes fails and rolls back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}, {Put, "b", make([]byte, 8)}, {Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	// Delete does not allocate a revision.
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
	// Failed batch leaves generation and revision untouched.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(snap, s.Snapshot()) {
		t.Fatal(e)
	}
	// Get on missing and invalid names.
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e = s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
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
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 640 || snap.NextRevision != 1281 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
}
