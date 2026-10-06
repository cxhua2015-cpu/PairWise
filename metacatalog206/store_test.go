package metacatalog206

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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	for _, name := range []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", name, e)
		}
		if _, _, e := s.Get(name); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: got %v", name, e)
		}
	}
	for _, name := range []string{"a", "0", "-", "_", "a-b_c-9"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: got %v", name, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, op := range []Op{
		{Kind(0), "a", nil}, {Kind(3), "a", nil}, {Delete, "a", []byte("x")},
	} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: got %v", op, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestCapacityOnlyAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch the total (4+2=6) exceeds the limit, but the final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1234")},
		{Put, "b", []byte("56")},
		{Put, "a", []byte("7")},
	}})
	if e != nil || r.Revision != 3 {
		t.Fatal(r, e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.NextRevision != 4 || snap.Generation != 1 {
		t.Fatal(snap)
	}
	// Final state exceeds total bytes: whole batch rolls back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(snap, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestMaxRecordsRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Replace within the same batch stays within MaxRecords.
	if _, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("w")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndRevisionNotConsumed(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Delete does not allocate a revision.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("w")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(r, e)
	}
}

func TestChangedSortedAndFinal(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")},
		{Put, "b", []byte("3")}, {Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r.Changed)
	}
}

func TestSnapshotDeepCopyAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'z'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "1" {
		t.Fatal(r, ok, e)
	}
}

func TestGetNotFound(t *testing.T) {
	s := store(t)
	r, ok, e := s.Get("nope")
	if e != nil || ok || r.Name != "" || r.Value != nil {
		t.Fatal(r, ok, e)
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
			k := fmt.Sprintf("k%02d", i)
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
	// Generation advanced once per successful non-empty batch: 32*20 puts + deletes.
	if snap.Generation != 32*20*2 {
		t.Fatal(snap.Generation)
	}
}

func TestConcurrentRevisionsUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 4096})
	const n = 16
	revs := make(chan uint64, n)
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			r, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%02d", i), []byte("v")}}})
			if e != nil {
				t.Error(e)
				return
			}
			revs <- r.Revision
		}()
	}
	w.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for rev := range revs {
		if seen[rev] {
			t.Fatal("duplicate revision", rev)
		}
		seen[rev] = true
	}
	if len(seen) != n {
		t.Fatal(len(seen))
	}
}
