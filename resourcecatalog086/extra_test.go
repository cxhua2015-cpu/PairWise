package resourcecatalog086

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
		{},
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: -1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: -2},
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
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	good := []string{"a", "abc", "a-1", "_", "z9_"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a/b", "é", "a.b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestValueAndUnknownKind(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Empty value is allowed.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits; final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "c", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 || r.Generation != 1 {
		t.Fatal(e, r)
	}
	// Final state exceeds limits: rollback.
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("33")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("5")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteSemantics(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	r2, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r2.Revision != r1.Revision {
		t.Fatal("delete must not allocate a revision", r1, r2, e)
	}
	if len(r2.Changed) != 0 {
		t.Fatal("deleted record must not appear in Changed", r2.Changed)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("record still present after delete")
	}
	// Put then delete the same name within one batch.
	r3, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "b", nil}}})
	if e != nil || len(r3.Changed) != 0 || len(s.Snapshot().Records) != 0 {
		t.Fatal(e, r3)
	}
}

func TestRevisionSequence(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	r2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r2.Revision != 3 || s.Snapshot().NextRevision != 4 {
		t.Fatal(r2, s.Snapshot())
	}
	rec, _, _ := s.Get("c")
	if rec.Revision != 3 {
		t.Fatal(rec)
	}
}

func TestGetInvalidName(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, _, e := s.Get("BAD!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("ok"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal("snapshot not sorted by name", snap)
	}
	snap.Records[0].Value[0] = 'X'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "1" {
		t.Fatal("snapshot shares memory with store")
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				if r, ok, e := s.Get(k); e == nil && ok && len(r.Value) != 1 {
					t.Error("bad value length")
				}
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal("expected empty store", snap)
	}
	// 32 goroutines x 21 non-empty successful batches.
	if snap.Generation != 32*21 {
		t.Fatal("generation mismatch", snap.Generation)
	}
	if snap.NextRevision != 32*20+1 {
		t.Fatal("revision mismatch", snap.NextRevision)
	}
}
