package imagecatalog

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
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil || x.Generation != 1 {
		t.Fatal(e, x)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Generation != 2 || x.Revision != 2 {
		t.Fatal(e, x)
	}
}

func TestDeleteDoesNotAllocateRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(e, x)
	}
	r, ok, _ := s.Get("b")
	if !ok || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestStructuralValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}, {Delete, "missing", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("extra")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNameAndValueBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("12")}}}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"", "abcd", "A", "a b", "a/b", "é"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("123")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	for _, name := range []string{"a-z", "a_z", "0-9", "---", "___"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, nil}}}); e != nil {
			t.Fatalf("name %q: %v", name, e)
		}
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}, {Delete, "a", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 1 {
		t.Fatal(s.Snapshot())
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("567")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("5")}, {Put, "d", []byte("6")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); len(snap.Records) != 1 || snap.Records[0].Name != "b" {
		t.Fatal(snap)
	}
}

func TestCapacityRollbackRestoresRevision(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("34")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(e, x)
	}
}

func TestChangedSortedAndDeleteExcluded(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("3")}, {Delete, "c", nil}, {Put, "a", []byte("4")}}})
	if e != nil {
		t.Fatal(e)
	}
	names := []string{x.Changed[0].Name, x.Changed[1].Name}
	if !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatal(x.Changed)
	}
	if x.Changed[0].Revision != 4 || string(x.Changed[0].Value) != "4" {
		t.Fatal(x.Changed[0])
	}
}

func TestGetErrors(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
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
			k := fmt.Sprintf("key-%02d", i)
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
	if len(snap.Records) != 32 || snap.NextRevision != snap.Generation+1 {
		t.Fatal(len(snap.Records), snap.Generation, snap.NextRevision)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
