package metacatalog326

import (
	"bytes"
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
		{},
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: -1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: -2},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	s := store(t)
	before := s.Snapshot()
	// Invalid op appears after ops that would otherwise touch state.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "ok", []byte("v")},
		{Kind(99), "x", nil},
	}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed despite structural failure")
	}
	// Unknown name delete would fail with ErrNotFound, but structural
	// errors anywhere in the batch must win.
	_, e = s.Apply(Batch{Ops: []Op{
		{Delete, "missing", nil},
		{Put, "BAD", []byte("v")},
	}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNameAndValueBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	good := "a-0_z"
	if _, e := s.Apply(Batch{Ops: []Op{{Put, good, bytes.Repeat([]byte("v"), 8)}}}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"", "A", "has space", "dot.", "1234567890123", "é"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok", bytes.Repeat([]byte("v"), 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ok", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("delete with value must be rejected")
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRevisionAllocationAndGeneration(t *testing.T) {
	s := store(t)
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || r1.Revision != 2 || r1.Generation != 1 {
		t.Fatal(e, r1)
	}
	// Delete does not allocate a revision: b got 2, not 3.
	r, ok, e := s.Get("b")
	if e != nil || !ok || r.Revision != 2 {
		t.Fatal(r, ok, e)
	}
	// Empty batch succeeds without bumping the generation.
	r2, e := s.Apply(Batch{})
	if e != nil || r2.Generation != 1 || r2.Revision != 2 {
		t.Fatal(e, r2)
	}
	// Failed batch leaves generation and revision untouched.
	if _, e = s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r3, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if e != nil || r3.Generation != 2 || r3.Revision != 3 {
		t.Fatal(e, r3)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 4 {
		t.Fatal(snap)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Intermediate state exceeds both limits, final state does not.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")},
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("12345678")}, // 3 records, 24 bytes mid-flight
		{Delete, "a", nil},
		{Delete, "b", nil}, // ends at 1 record, 8 bytes
	}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "c" {
		t.Fatal(e, x)
	}
	// Final state over the record limit fails and rolls back.
	before := s.Snapshot()
	if _, e = s.Apply(Batch{Ops: []Op{
		{Put, "d", []byte("1")},
		{Put, "e", []byte("1")},
		{Put, "f", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("capacity failure must roll back")
	}
	// Final state over the total-value limit fails and rolls back.
	if _, e = s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("12345678")},
		{Put, "d", []byte("12345678")},
		{Put, "e", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestChangedReportsFinalStatePerName(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Put, "b", []byte("3")},
		{Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Only names that exist at batch end appear, sorted by name.
	if len(x.Changed) != 1 || x.Changed[0].Name != "b" || x.Changed[0].Revision != 3 || string(x.Changed[0].Value) != "3" {
		t.Fatal(x.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "z", []byte("1")},
		{Put, "m", []byte("2")},
		{Put, "a", []byte("3")},
	}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "m" || snap.Records[2].Name != "z" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "3" {
		t.Fatal("snapshot shares memory with store")
	}
	if _, ok, _ = s.Get("nope"); ok {
		t.Fatal("missing key reported found")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i%16)
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
	seen := map[uint64]bool{}
	for _, r := range snap.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[r.Revision] = true
	}
}
