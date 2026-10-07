package metacatalog371

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	good := []string{"a", "abc", "a-1", "z_9", "0-_"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a B", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s := store(t)
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

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Structurally invalid op after a Delete of a missing name: structural
	// validation must win over state errors.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Kind(7), "x", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state has 2 records and 6 total bytes; final state fits.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("aa")},
		{Put, "b", []byte("bbbb")},
		{Delete, "b", nil},
	}}); e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("aa")}}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("bb")}, {Put, "c", []byte("cc")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("aaaa")}, {Put, "b", []byte("bbb")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after total-bytes failure")
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s := store(t)
	r0, e := s.Apply(Batch{})
	if e != nil || r0.Generation != 0 || r0.Revision != 0 || r0.Changed != nil {
		t.Fatal(r0, e)
	}
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}, {Delete, "a", nil}, {Put, "b", nil}}})
	if e != nil || r1.Generation != 1 || r1.Revision != 2 {
		t.Fatal(r1, e)
	}
	if len(r1.Changed) != 1 || r1.Changed[0].Name != "b" || r1.Changed[0].Revision != 2 {
		t.Fatal(r1.Changed)
	}
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
	// Failed batch must not advance generation or revision.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(s.Snapshot(), snap) {
		t.Fatal("generation or revision advanced on failure")
	}
}

func TestChangedSortedAndFinalOnly(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")},
		{Put, "a", []byte("1")},
		{Put, "b", []byte("1")},
		{Delete, "c", nil},
		{Put, "a", []byte("2")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r.Changed)
	}
	if string(r.Changed[0].Value) != "2" || r.Changed[0].Revision != 4 {
		t.Fatal(r.Changed[0])
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("x")}, {Put, "a", []byte("y")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "y" {
		t.Fatal(r, ok, e)
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 640 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	// Revisions are contiguous: 640 batches * 2 puts each.
	if snap.NextRevision != 1281 {
		t.Fatal(snap.NextRevision)
	}
}
