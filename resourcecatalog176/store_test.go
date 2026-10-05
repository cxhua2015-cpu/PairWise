package resourcecatalog176

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	for _, bad := range []string{"", "A", "a b", "a/b", "a.b", "é", "this-name-is-too-long"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", bad, e)
		}
	}
	s2, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 128})
	for _, ok := range []string{"a", "0", "-", "_", "abc-123_xyz", "123456789012"} {
		if _, e := s2.Apply(Batch{Ops: []Op{{Put, ok, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", ok, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing key would be ErrNotFound, but a later invalid op
	// must win because structural validation runs before any state read.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Exceeds total value bytes only at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Exceeds record count.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}, {Put, "d", []byte("y")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "a", []byte("w")}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	// Failed batch must not bump generation or revision.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
	// Delete-only batch bumps generation once, allocates no revision.
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "2" {
		t.Fatal(e, ok, r)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(e, ok)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%d", i%16)
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
	if snap.Generation == 0 || snap.NextRevision == 1 {
		t.Fatal(snap)
	}
	for _, r := range snap.Records {
		if !validName(r.Name, 16) || len(r.Value) > 8 {
			t.Fatal(r)
		}
	}
}
