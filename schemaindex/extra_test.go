package schemaindex

import (
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

func TestNameCharset(t *testing.T) {
	s := store(t)
	good := []string{"a", "0", "a-b_c", "a1"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}, {Delete, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "a/b", "this-name-is-way-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
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

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing name would be ErrNotFound, but structural
	// validation of a later op must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	b := s.Snapshot()
	// Exceeds MaxRecords=3 at batch end.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "c", []byte("2")}, {Put, "d", []byte("3")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Exceeds MaxTotalValueBytes=16 at batch end.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 1 {
		t.Fatal(got)
	}
}

func TestDeleteMissingAndReput(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "k", []byte("1")}, {Delete, "k", nil}, {Put, "k", []byte("2")}}})
	if x.Revision != 2 || len(x.Changed) != 1 || string(x.Changed[0].Value) != "2" {
		t.Fatal(x)
	}
	r, ok, _ := s.Get("k")
	if !ok || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
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
			k := fmt.Sprintf("k%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
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
	for i, r := range snap.Records {
		if i > 0 && snap.Records[i-1].Name >= r.Name {
			t.Fatal("not sorted")
		}
	}
	if snap.NextRevision != snap.Records[len(snap.Records)-1].Revision+1 && snap.NextRevision <= 1 {
		t.Fatal(snap.NextRevision)
	}
}
