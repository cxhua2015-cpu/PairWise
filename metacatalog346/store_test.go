package metacatalog346

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
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t) // MaxNameBytes 12
	ok := []string{"a", "abc-09_x", "abcdefghijkl"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcdefghijklm", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNotFoundAndNoRevisionAlloc(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
	if rec, ok, _ := s.Get("b"); !ok || rec.Revision != 2 {
		t.Fatal(rec, ok)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
	}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	// Exceeds total value bytes only at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Exceeds record count at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "d", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after failed batches")
	}
	// Delete-then-put within one batch passes final capacity checks.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Delete, "a", nil}, {Delete, "b", nil},
		{Put, "c", []byte("12345678")}, {Put, "d", []byte("12345678")},
	}}); e != nil {
		t.Fatal(e)
	}
}

func TestOverwriteRevisionAndChanged(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "a", []byte("2")}}})
	if e != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Revision != 2 {
		t.Fatal(e, r)
	}
	if string(r.Changed[0].Value) != "2" {
		t.Fatal(r.Changed[0])
	}
}

func TestGetInvalidName(t *testing.T) {
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
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "xy" {
		t.Fatal("snapshot shares memory with store")
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("z")}}}); e != nil {
		t.Fatal(e)
	}
	if len(snap.Records) != 1 {
		t.Fatal("snapshot mutated by later apply")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 512})
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
			for n := 0; n < 50; n++ {
				if n%5 == 4 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
				} else {
					_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				}
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || len(snap.Records) > 16 {
		t.Fatal(snap.Generation, len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
