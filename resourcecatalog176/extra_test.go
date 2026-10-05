package resourcecatalog176

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
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
	good := []string{"a", "abc", "a-1", "_z9", "0-_"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a/b", "é", "a.b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("no"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestUnknownKindAndValueLimits(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != 0 || got.NextRevision != 1 || len(got.Records) != 0 {
		t.Fatal(got)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	// Structurally invalid op later in the batch must win over the
	// state-dependent not-found error of the earlier op.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTotalValueCapacityRollback(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 5})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("345")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Overwrite shrinks usage; final total 3 fits.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
	// Final total 6 exceeds 5: whole batch rolls back, revision not consumed.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1234")}, {Put, "c", []byte("12")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	got := s.Snapshot()
	if got.Generation != b.Generation+1 || got.NextRevision != b.NextRevision+1 {
		t.Fatal(got)
	}
	if _, ok, _ := s.Get("c"); ok {
		t.Fatal("c must not exist")
	}
}

func TestRecordCapacityRollback(t *testing.T) {
	s, e := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if r, ok, _ := s.Get("a"); !ok || string(r.Value) != "v" {
		t.Fatal(r, ok)
	}
	// Delete-then-put in one batch succeeds because limits apply at the end.
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("w")}}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(x, e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "a", []byte("w")}}})
	if e != nil || x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x, e)
	}
	if got := s.Snapshot(); got.Generation != 1 || got.NextRevision != 3 {
		t.Fatal(got)
	}
	// Failed batch must not bump generation or revision.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != 1 || got.NextRevision != 3 {
		t.Fatal(got)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, ok, e := s.Get("a")
	if e != nil || !ok || !bytes.Equal(r.Value, []byte("2")) {
		t.Fatal(r, ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	if e != nil {
		t.Fatal(e)
	}
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
	got := s.Snapshot()
	if len(got.Records) != 32 || got.Generation != 32*20 {
		t.Fatal(len(got.Records), got.Generation)
	}
	if got.NextRevision != 1+uint64(32*20*2) {
		t.Fatal(got.NextRevision)
	}
}
