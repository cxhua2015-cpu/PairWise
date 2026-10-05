package resourcecatalog161

import (
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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	ok := []string{"a", "z9-_", "abc-def_123"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a/b", "é", "a.b", "toolongname123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeState(t *testing.T) {
	s := store(t)
	// Delete of missing name would fail at state read, but structural
	// error in a later op must win because validation runs first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Transient overflow within the batch is fine; only final state counts.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")}, // transiently 3 records / 6 bytes
		{Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 0 {
		t.Fatal("rollback failed", got)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	r2, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r2.Revision != r1.Revision {
		t.Fatal(e, r1, r2)
	}
	r3, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}}})
	if r3.Revision != r1.Revision+1 || r3.Generation != r2.Generation+1 {
		t.Fatal(r1, r2, r3)
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

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
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
			k := fmt.Sprintf("k%03d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// Revisions are contiguous: 32 keys * 20 rounds * 2 puts = 1280.
	if snap.NextRevision != 1281 {
		t.Fatal(snap.NextRevision)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
