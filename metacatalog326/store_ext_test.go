package metacatalog326

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	cases := []Options{
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
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

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
	good := []string{"a", "z09", "a-b", "a_b", "123", "---"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, op := range []Op{
		{Kind: 0, Name: "a"},
		{Kind: 3, Name: "a"},
		{Kind: 99, Name: "a"},
		{Kind: Delete, Name: "a", Value: []byte("x")},
	} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: want ErrInvalidInput, got %v", op, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op deletes a missing name.
	// Structural validation must win over the stateful ErrNotFound.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	// Mid-batch there are 3 records and 6 value bytes; final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "a", nil},
		{Put, "c", []byte("5")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 2 || x.Revision != 4 {
		t.Fatal(x, s.Snapshot())
	}
}

func TestCapacityRollback(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}})
	if e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34")}, {Put, "c", []byte("56")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	if got := s.Snapshot(); got.Generation != r1.Generation || got.NextRevision != 2 {
		t.Fatal("generation/revision leaked after rollback", got)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34567")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal("total value bytes limit not enforced:", e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
	x, e = s.Apply(Batch{})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal("empty batch must not bump generation", x, e)
	}
}

func TestGenerationAndRevisionSemantics(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	// Delete allocates no revision.
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Generation != 2 || x.Revision != 2 {
		t.Fatal(x)
	}
	if got := s.Snapshot(); got.Generation != 2 || got.NextRevision != 3 {
		t.Fatal(got)
	}
}

func TestChangedContents(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Delete, "b", nil},
		{Put, "c", []byte("3")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Deleted records are absent; survivors sorted by name with final revision.
	if len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[1].Name != "c" {
		t.Fatal(x.Changed)
	}
	if x.Changed[0].Revision != 2 || x.Changed[1].Revision != 3 {
		t.Fatal(x.Changed)
	}
}

func TestGetErrors(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}, {Put, "b", []byte("y")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get(snap.Records[0].Name)
	if r.Value[0] == 'Q' {
		t.Fatal("snapshot aliases internal state")
	}
	names := []string{snap.Records[0].Name, snap.Records[1].Name}
	if names[0] != "a" || names[1] != "b" {
		t.Fatal("snapshot not sorted by name", names)
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
			for j := 0; j < 50; j++ {
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
	// Each key was put 100 times; revisions are consecutive per batch.
	if snap.NextRevision != 1+32*50*2 {
		t.Fatal(snap.NextRevision)
	}
	if snap.Generation != 32*50 {
		t.Fatal(snap.Generation)
	}
}
