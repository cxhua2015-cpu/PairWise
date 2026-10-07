package metacatalog361

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
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "has space", "dot.name", "中文", "toolongname123", "UPPER", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: got %v", n, e)
		}
	}
	good := []string{"a", "0", "-", "_", "a-b_c-9", "abcdefghijkl"}
	for _, n := range good {
		if _, _, e := s.Get(n); e != nil {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndValueTooLong(t *testing.T) {
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
	// Structurally invalid op comes after an op that would fail state checks;
	// structural validation must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	// Transiently exceeds record capacity mid-batch but fine at the end.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("x")}, {Put, "b", []byte("y")}, {Put, "c", []byte("z")}, {Delete, "a", nil},
	}}); e != nil {
		t.Fatal(e)
	}
	// Transiently exceeds total value bytes mid-batch but fine at the end.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1234")}, {Put, "b", []byte("12")}, {Put, "c", []byte("34")},
	}}); e != nil {
		t.Fatal(e)
	}
	// Final record count exceeded.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("q")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final total value bytes exceeded.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("1234")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "b" || snap.Records[1].Name != "c" {
		t.Fatal(snap)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s := store(t)
	if g := s.Snapshot(); g.Generation != 0 || g.NextRevision != 1 {
		t.Fatal(g)
	}
	// Empty batch: success, generation unchanged.
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if e != nil || x.Generation != 1 || x.Revision != 1 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	// Delete does not allocate a revision.
	x, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "b", nil}}})
	if e != nil || x.Generation != 2 || x.Revision != 3 || len(x.Changed) != 1 || x.Changed[0].Name != "c" || x.Changed[0].Revision != 3 {
		t.Fatal(e, x)
	}
	// Failed batch does not consume revisions or generation.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if e != nil || x.Generation != 3 || x.Revision != 4 {
		t.Fatal(e, x)
	}
	if g := s.Snapshot(); g.Generation != 3 || g.NextRevision != 5 {
		t.Fatal(g)
	}
}

func TestGetNotFoundAndIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(e, ok)
	}
	in := []byte("orig")
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "k", in}}}); e != nil {
		t.Fatal(e)
	}
	in[0] = 'X'
	r, ok, _ := s.Get("k")
	if !ok || string(r.Value) != "orig" {
		t.Fatal(r, ok)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Y'
	r2, _, _ := s.Get("k")
	if string(r2.Value) != "orig" {
		t.Fatal(r2)
	}
}

func TestSnapshotSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "delta", []byte("1")}, {Put, "alpha", []byte("2")}, {Put, "charlie", []byte("3")}, {Put, "bravo", []byte("4")},
	}}); e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, r := range s.Snapshot().Records {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"alpha", "bravo", "charlie", "delta"}) {
		t.Fatal(names)
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*20 || snap.NextRevision != 32*20*2+1 {
		t.Fatal(len(snap.Records), snap.Generation, snap.NextRevision)
	}
}
