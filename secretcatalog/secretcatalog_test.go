package secretcatalog

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

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	for _, n := range []string{"", "A", "a b", "a.b", "a/b", "é", "abcdefghijklm"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_9", "abcdefghijkl"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
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

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing key would be ErrNotFound, but a later structural
	// error must win because validation precedes any state read.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	// Delete does not allocate a revision.
	r, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "b", nil}, {Put, "c", []byte("3")}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	// Empty batch: generation unchanged.
	b := s.Snapshot()
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != b.Generation || s.Snapshot().Generation != b.Generation {
		t.Fatal(e, r)
	}
	// Failed batch: generation and revision unchanged.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) || s.Snapshot().NextRevision != 4 || s.Snapshot().Generation != 2 {
		t.Fatal(e, s.Snapshot())
	}
}

func TestEndOfBatchCapacity(t *testing.T) {
	// Intermediate states may exceed limits; only the final state counts.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Put, "c", []byte("33")},
		{Delete, "a", nil}, {Delete, "b", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Final state exceeds record capacity.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Put, "y", []byte("2")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final state exceeds total value bytes.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3333")}, {Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Overwrite accounting: replacing a value only charges the delta.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("333")}}})
	if e != nil {
		t.Fatal(e)
	}
	if got := len(s.Snapshot().Records); got != 1 {
		t.Fatal(got)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(string(r.Value))
	}
	if snap.NextRevision != 4 || snap.Generation != 1 {
		t.Fatal(snap)
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

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	// Each goroutine committed 40 non-empty batches.
	if snap.Generation != 32*40 {
		t.Fatal(snap.Generation)
	}
	// 20 Puts per goroutine, all revisions unique and contiguous.
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}
