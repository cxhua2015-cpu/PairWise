package artifactindex

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
			t.Fatalf("opts=%+v err=%v", o, e)
		}
	}
}

func TestNameAndValueValidation(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Bad", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "toolongname123", []byte("v")},
		{Put, "a", make([]byte, 9)},
		{Delete, "bad?", nil},
		{Kind(0), "a", nil},
		{Kind(3), "a", nil},
	}
	for _, op := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v err=%v", op, e)
		}
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := s.Get(""); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
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
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")},
		{Put, "b", []byte("12345678")},
	}}); e != nil {
		t.Fatal(e)
	}
	// Total value bytes exceeded only at batch end: delete frees space first.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Delete, "a", nil},
		{Put, "c", []byte("1234")},
		{Put, "d", []byte("1234")},
	}}); e != nil {
		t.Fatal(e)
	}
	// Record count exceeded at batch end -> full rollback.
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "e", []byte("1")},
		{Put, "f", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Total bytes exceeded at batch end -> full rollback.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "e", []byte("12345678")},
		{Put, "f", []byte("12345678")},
		{Put, "g", []byte("12345678")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestDeleteSemantics(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	// Double delete in one batch fails and rolls back the first delete.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("a"); !ok {
		t.Fatal("delete not rolled back")
	}
	// Put then delete in one batch: name absent from Changed.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}, {Delete, "b", nil}}})
	if e != nil || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	if _, ok, _ := s.Get("b"); ok {
		t.Fatal("b should be deleted")
	}
}

func TestRevisionContiguity(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")},
		{Delete, "a", nil},
		{Put, "b", []byte("2")},
		{Put, "c", []byte("3")},
	}})
	if e != nil || x.Revision != 3 {
		t.Fatal(x, e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 4 || len(snap.Records) != 2 {
		t.Fatal(snap)
	}
	if snap.Records[0].Name != "b" || snap.Records[1].Name != "c" {
		t.Fatal("records not sorted by name", snap)
	}
	if snap.Records[0].Revision != 2 || snap.Records[1].Revision != 3 {
		t.Fatal("revisions not contiguous", snap)
	}
}

func TestGetNotFoundAndIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	v := []byte("abc")
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", v}}}); e != nil {
		t.Fatal(e)
	}
	v[0] = 'z' // caller mutation must not leak in
	r, _, _ := s.Get("a")
	if string(r.Value) != "abc" {
		t.Fatal(r)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, _, _ = s.Get("a")
	if string(r.Value) != "abc" {
		t.Fatal("snapshot mutation leaked into store")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, "zz", nil}}})
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 {
		t.Fatal(len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
	if snap.NextRevision <= snap.Generation {
		t.Fatal(snap)
	}
}
