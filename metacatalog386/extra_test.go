package metacatalog386

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
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	for _, bad := range []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	for _, ok := range []string{"a", "z9_", "abc"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, ok, nil}}}); e != nil {
			t.Fatalf("name %q: %v", ok, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "b", []byte("12")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// total value bytes would become 8 > 6
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// record count would become 3 > 2
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", nil}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failures")
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s := store(t)
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}})
	if e != nil || r1.Revision != 1 {
		t.Fatal(e, r1)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", nil}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r2, e := s.Apply(Batch{Ops: []Op{{Put, "b", nil}}})
	if e != nil || r2.Revision != 2 || r2.Generation != 2 {
		t.Fatal(e, r2)
	}
	if snap := s.Snapshot(); snap.NextRevision != 3 || snap.Generation != 2 {
		t.Fatal(snap)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
}

func TestDeleteThenPutSameName(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("zz")}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Revision != 2 {
		t.Fatal(e, r)
	}
	rec, ok, _ := s.Get("a")
	if !ok || string(rec.Value) != "zz" || rec.Revision != 2 {
		t.Fatal(rec, ok)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
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
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'q'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestApplyInputValueCopied(t *testing.T) {
	s := store(t)
	v := []byte("ab")
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", v}}}); e != nil {
		t.Fatal(e)
	}
	v[0] = 'z'
	rec, _, _ := s.Get("a")
	if string(rec.Value) != "ab" {
		t.Fatal("store aliases caller buffer")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%03d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || snap.NextRevision < 1 {
		t.Fatal(snap)
	}
	for _, r := range snap.Records {
		if r.Revision >= snap.NextRevision {
			t.Fatal("record revision beyond next revision")
		}
	}
}
