package metacatalog321

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
	cases := []Options{
		{},
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: -1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestNameAndKindValidation(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Upper", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "dot.name", []byte("v")},
		{Put, "中文", []byte("v")},
		{Put, "this-name-is-way-too-long", []byte("v")},
		{Kind(0), "a", []byte("v")},
		{Kind(99), "a", []byte("v")},
		{Put, "a", []byte("value-longer-than-8-bytes")},
	}
	for _, op := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: want ErrInvalidInput, got %v", op, e)
		}
	}
	// Valid characters: lowercase, digits, hyphen, underscore.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a0-_", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing name would be ErrNotFound, but the structurally
	// invalid op later in the batch must win because validation comes first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	// Intermediate state exceeds both limits, final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Generation != 1 || r.Revision != 3 {
		t.Fatal(r)
	}
	// Final state exceeds record limit.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final state exceeds total value bytes.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("4444")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Failed batches roll back: b still holds "22", generation unchanged.
	rec, ok, _ := s.Get("b")
	if !ok || string(rec.Value) != "22" || s.Snapshot().Generation != 1 {
		t.Fatal(rec, ok, s.Snapshot())
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	// Delete-only success bumps generation once, revision unchanged.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Generation != 2 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestChangedSortedAndDeduped(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")},
		{Put, "a", []byte("2")},
		{Put, "b", []byte("3")},
		{Delete, "c", nil},
		{Put, "a", []byte("4")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, rec := range r.Changed {
		names = append(names, rec.Name)
	}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	// Deleted record reported with zero revision; last put wins.
	if r.Changed[0].Revision != 4 || string(r.Changed[0].Value) != "4" {
		t.Fatal(r.Changed[0])
	}
	if r.Changed[2].Revision != 0 || len(r.Changed[2].Value) != 0 {
		t.Fatal(r.Changed[2])
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 3 {
		t.Fatal(snap.NextRevision)
	}
	snap.Records[0].Value[0] = 'z'
	snap.Records = snap.Records[:0]
	again := s.Snapshot()
	if len(again.Records) != 2 || again.Records[0].Name != "a" || string(again.Records[0].Value) != "x" {
		t.Fatal(again.Records)
	}
	// Result.Changed values are isolated too.
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("q")}}})
	r.Changed[0].Value[0] = 'z'
	rec, _, _ := s.Get("a")
	if !bytes.Equal(rec.Value, []byte("q")) {
		t.Fatal(rec)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
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
			name := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				_, _, _ = s.Get(name)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || snap.NextRevision == 0 {
		t.Fatal(snap)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}

func TestConcurrentMonotonicRevision(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	const writers = 8
	revisions := make(chan uint64, writers*20)
	var w sync.WaitGroup
	for i := 0; i < writers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			name := fmt.Sprintf("k%d", i)
			for j := 0; j < 20; j++ {
				r, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}})
				if e != nil {
					t.Error(e)
					return
				}
				revisions <- r.Revision
			}
		}()
	}
	w.Wait()
	close(revisions)
	seen := make(map[uint64]bool)
	for rev := range revisions {
		if seen[rev] {
			t.Fatalf("duplicate revision %d", rev)
		}
		seen[rev] = true
	}
	if len(seen) != writers*20 {
		t.Fatal(len(seen))
	}
	if got := s.Snapshot().NextRevision; got != uint64(writers*20)+1 {
		t.Fatal(got)
	}
}
