package resourcecatalog196

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
	r, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || s.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestDeleteDoesNotAllocateRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("y")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
	rec, ok, _ := s.Get("b")
	if !ok || rec.Revision != 2 {
		t.Fatal(rec, ok)
	}
	if s.Snapshot().NextRevision != 3 {
		t.Fatal(s.Snapshot())
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, n := range []string{"", "A", "a b", "a.b", "中文", "a/b", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "0", "a-b_c", "z9-_" + "12345678"[:8]} {
		fresh := store(t)
		if _, e := fresh.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("x"), 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityEndOfBatchOnly(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch 3 records / 6 bytes would exceed limits, but final state fits.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 1 || snap.Records[0].Name != "b" {
		t.Fatal(snap)
	}
}

func TestCapacityExceeded(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("v")}, {Put, "b", []byte("v")}, {Put, "c", []byte("v")}, {Put, "d", []byte("v")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("v"), 8)}, {Put, "b", bytes.Repeat([]byte("v"), 8)}, {Put, "c", []byte("v")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestRollbackKeepsRevisionAndGeneration(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "missing", nil}}})
	after := s.Snapshot()
	if after.Generation != b.Generation || after.NextRevision != b.NextRevision || len(after.Records) != 1 {
		t.Fatal(b, after)
	}
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("z")}}})
	if r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	snap.Records[0].Name = "zzz"
	again := s.Snapshot()
	if again.Records[0].Name != "a" || string(again.Records[0].Value) != "2" {
		t.Fatal(again)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
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
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("w")}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	if snap.Generation != 32*50*2 {
		t.Fatal(snap.Generation)
	}
	// Revisions are contiguous: 32 keys * 100 puts each.
	if snap.NextRevision != 32*100+1 {
		t.Fatal(snap.NextRevision)
	}
}
