package resourcecatalog126

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestRevisionGapsAndRollback(t *testing.T) {
	s := store(t)
	// Delete does not allocate a revision.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if e != nil || r.Revision != 3 || r.Generation != 2 {
		t.Fatal(e, r)
	}
	rec, ok, _ := s.Get("c")
	if !ok || rec.Revision != 3 {
		t.Fatal(rec, ok)
	}
	// Failed batch must not consume revisions or generation.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("x")}, {Delete, "missing", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("y")}}})
	if e != nil || r.Revision != 4 || r.Generation != 3 {
		t.Fatal(e, r)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, but final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")},
		{Delete, "a", nil}, {Delete, "b", nil},
	}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "c" {
		t.Fatal(e, r)
	}
	// Final state exceeds record limit -> ErrCapacity, full rollback.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Put, "e", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
	// Final state exceeds total value bytes -> ErrCapacity.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("4444")}, {Put, "d", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestValidationBeforeState(t *testing.T) {
	s := store(t)
	// Invalid op later in the batch must fail even though an earlier op
	// would fail with ErrNotFound: structural validation comes first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "BAD!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e = s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(e, ok)
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, n := range []string{"abc", "a-b_c", "aaaaaaaaaaaa"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"", "A", "a b", "a/b", "a.b", "é", "this-name-is-too-long"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	if snap.NextRevision != 3 || snap.Generation != 1 {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
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
	if len(snap.Records) != 32 || snap.Generation != 32*20 || snap.NextRevision != 32*20*2+1 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
}
