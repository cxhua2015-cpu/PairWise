package endpointcatalog

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, bad := range []string{"", "A", "a b", "a.b", "中文", "a/b", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok-name_1", []byte("v")}}}); e != nil {
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
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op would hit ErrNotFound
	// if state were read first. Structural validation must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil || len(r.Changed) != 3 {
		t.Fatal(e, r)
	}
	if got := len(s.Snapshot().Records); got != 1 {
		t.Fatal(got)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("22")}, {Put, "c", []byte("33")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2222")}, {Put, "c", []byte("2222")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = s.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s := store(t)
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r.Revision != 2 {
		t.Fatal(r.Revision)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if r.Revision != 3 {
		t.Fatal(r.Revision)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 4 || snap.Generation != 2 {
		t.Fatal(snap)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Delete, "zz", nil}}})
	if s.Snapshot().NextRevision != 4 {
		t.Fatal(s.Snapshot().NextRevision)
	}
}

func TestDeleteMissing(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGetAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	in := []byte("ab")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", in}}})
	in[0] = 'X'
	r, ok, e := s.Get("k")
	if e != nil || !ok || string(r.Value) != "ab" {
		t.Fatal(e, ok, r)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'Y'
	r, _, _ = s.Get("k")
	if string(r.Value) != "ab" {
		t.Fatal(string(r.Value))
	}
	if _, ok, _ := s.Get("missing"); ok {
		t.Fatal("expected miss")
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSorted(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("1")}, {Put, "b", []byte("1")},
	}})
	recs := s.Snapshot().Records
	if len(recs) != 3 || recs[0].Name != "a" || recs[1].Name != "b" || recs[2].Name != "c" {
		t.Fatal(recs)
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
			k := fmt.Sprintf("key-%02d", i%16)
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
	if snap.NextRevision != snap.Generation+1 && snap.NextRevision < 1 {
		t.Fatal(snap)
	}
}

func TestConcurrentRevisionMonotonic(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 25; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
			}
		}()
	}
	w.Wait()
	if got := s.Snapshot().NextRevision; got != 16*25+1 {
		t.Fatal(got)
	}
}
