package resourcecatalog151

import (
	"bytes"
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
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	good := []string{"a", "z9-_", "abc-def_123"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "a/b", "UPPER"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestNameAndValueLimits(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcd", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatal(k, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would yield ErrNotFound, but the structural
	// error later in the batch must win because validation comes first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r.Changed)
	}
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Exceeds both record count and total value bytes at batch end.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("22")}, {Put, "c", []byte("33")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
	// Revision counter must be rolled back: next Put gets revision 2.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestDeleteRollbackRevision(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	// Put allocates revision 2, then Delete fails: revision must roll back.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if s.Snapshot().NextRevision != 2 {
		t.Fatal(s.Snapshot())
	}
}

func TestOverwriteAccounting(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}}}); e != nil {
		t.Fatal(e)
	}
	// Overwrite keeps total at 4, still within capacity.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("wxyz")}}}); e != nil {
		t.Fatal(e)
	}
	// Delete then Put of a different key nets to the same total.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("wxyz")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGetErrors(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "v" {
		t.Fatal(r)
	}
}

func TestSnapshotSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}}); e != nil {
		t.Fatal(e)
	}
	recs := s.Snapshot().Records
	names := []string{recs[0].Name, recs[1].Name, recs[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
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
			k := fmt.Sprintf("k%03d", i)
			v := []byte{byte(i)}
			if _, e := s.Apply(Batch{Ops: []Op{{Put, k, v}}}); e != nil {
				t.Error(e)
				return
			}
			v[0] = 0xff // mutating caller buffer must not affect stored value
			r, ok, e := s.Get(k)
			if e != nil || !ok || !bytes.Equal(r.Value, []byte{byte(i)}) {
				t.Error(r, ok, e)
			}
			_ = s.Snapshot()
			if _, e := s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("z")}}}); e != nil {
				t.Error(e)
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 64 || snap.NextRevision != 65 {
		t.Fatal(len(snap.Records), snap.Generation, snap.NextRevision)
	}
}
