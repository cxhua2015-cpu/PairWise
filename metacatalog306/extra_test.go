package metacatalog306

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "this-name-is-way-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: want ErrInvalidInput, got %v", n, e)
		}
	}
	good := []string{"a", "z-0_9", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueTooLong(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("x"), 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Structurally invalid op later in the batch must win over an earlier
	// state-dependent failure (Delete of a missing name).
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestDeleteMissingRollback(t *testing.T) {
	s := store(t)
	r1, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}, {Delete, "ghost", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state not rolled back")
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r2.Generation != r1.Generation+1 || r2.Revision != 3 {
		t.Fatalf("generation/revision leaked: %+v -> %+v", r1, r2)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, final state fits.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}})
	b := s.Snapshot()
	// Too many records.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "c", []byte("2")}, {Put, "d", []byte("3")},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Total value bytes exceeded.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state not rolled back after capacity failure")
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = s.Apply(Batch{Ops: []Op{}})
	if r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r)
	}
}

func TestChangedOrderAndRevision(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")},
		{Delete, "c", nil}, {Put, "a", []byte("4")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Revision != 4 || len(r.Changed) != 2 {
		t.Fatal(r)
	}
	if r.Changed[0].Name != "a" || r.Changed[0].Revision != 4 || string(r.Changed[0].Value) != "4" {
		t.Fatal(r.Changed[0])
	}
	if r.Changed[1].Name != "b" || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed[1])
	}
	if _, ok, _ := s.Get("c"); ok {
		t.Fatal("c should be deleted")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}})
	snap := s.Snapshot()
	if snap.NextRevision != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
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
			k := fmt.Sprintf("k%02d", i)
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
	seen := make(map[uint64]bool)
	for _, r := range snap.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[r.Revision] = true
	}
	if snap.NextRevision != 32*20*2+1 {
		t.Fatal(snap.NextRevision)
	}
}
