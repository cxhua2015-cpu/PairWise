package metacatalog376

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameAndKindValidation(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "A", []byte("v")},
		{Put, "a b", []byte("v")},
		{Put, "a/b", []byte("v")},
		{Put, "toolongname123", []byte("v")},
		{Put, "ok", make([]byte, 9)},
		{Delete, "a", []byte("x")},
		{Kind(0), "a", nil},
		{Kind(3), "a", nil},
	}
	for _, op := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	for _, n := range []string{"a-1_b", "z", "0"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op would fail with ErrNotFound
	// if state were read first. Structural validation must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestDeleteKeepsRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Generation != 2 || x.Revision != 1 {
		t.Fatal(x, e)
	}
	if sn := s.Snapshot(); sn.NextRevision != 2 || len(sn.Records) != 0 {
		t.Fatal(sn)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}})
	b := s.Snapshot()
	// Exceeds MaxTotalValueBytes=16 only at batch end.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if sn := s.Snapshot(); sn.Generation != b.Generation || sn.NextRevision != b.NextRevision || len(sn.Records) != 1 {
		t.Fatal(sn)
	}
	// Record-count capacity.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", nil}, {Put, "c", nil}, {Put, "d", nil}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
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

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	sn := s.Snapshot()
	if len(sn.Records) != 3 || sn.Records[0].Name != "a" || sn.Records[1].Name != "b" || sn.Records[2].Name != "c" {
		t.Fatal(sn)
	}
	sn.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("k%03d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	sn := s.Snapshot()
	if len(sn.Records) != 16 {
		t.Fatal(len(sn.Records))
	}
	for i, r := range sn.Records {
		want := fmt.Sprintf("k%03d", i)
		if r.Name != want {
			t.Fatalf("record %d = %q, want %q", i, r.Name, want)
		}
	}
}
