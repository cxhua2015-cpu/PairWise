package metacatalog366

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
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

func TestNameAndValueValidation(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Bad", []byte("v")},
		{Put, "a b", []byte("v")},
		{Put, "a.b", []byte("v")},
		{Put, "toolongname123", []byte("v")},
		{Put, "ok", make([]byte, 9)},
		{Kind(0), "ok", nil},
		{Kind(3), "ok", nil},
	}
	for _, op := range bad {
		before := s.Snapshot()
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
		if !reflect.DeepEqual(before, s.Snapshot()) {
			t.Fatalf("op %+v mutated state", op)
		}
	}
	s, _ = New(Options{MaxRecords: 8, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	for _, name := range []string{"a", "z9", "a-b_c", "123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); e != nil {
			t.Fatalf("name %q rejected: %v", name, e)
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
	x, e = s.Apply(Batch{Ops: nil})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
}

func TestRevisionAndGenerationSemantics(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Generation != 2 || x.Revision != 2 || len(x.Changed) != 0 {
		t.Fatal(x)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 3 || len(snap.Records) != 1 || snap.Records[0].Name != "b" {
		t.Fatal(snap)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch the total exceeds the limit; the batch must still succeed.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "c", nil},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(x, e)
	}
	// Exceeding the total at batch end fails and rolls back.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
	// Exceeding the record count at batch end fails and rolls back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "d", []byte("2")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndGet(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("missing"); e != nil {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("missing"); ok {
		t.Fatal("unexpected hit")
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 || snap.Generation != 32*50 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	if snap.NextRevision != snap.Generation*2+1 {
		t.Fatal(snap.NextRevision)
	}
}
