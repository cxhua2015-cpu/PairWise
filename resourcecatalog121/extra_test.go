package resourcecatalog121

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	s := store(t)
	// Invalid op appears after a valid one: nothing may be applied.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("v")}, {Put, "BAD", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("ok"); ok {
		t.Fatal("state read/mutated before full validation")
	}
	// Unknown kind.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Empty name, oversized name, oversized value.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	long := make([]byte, 13)
	for i := range long {
		long[i] = 'a'
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, string(long), nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Invalid names rejected by Get too.
	if _, _, e := s.Get("Bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	// Failing batch allocates revisions internally but must roll them back.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "ghost", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Put, "b", []byte("2")},
		{Put, "c", []byte("3")},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 {
		t.Fatal(e, r)
	}
	// Final record count exceeded -> ErrCapacity and full rollback.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Put, "y", []byte("1")}, {Put, "z", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
	// Final total value bytes exceeded -> ErrCapacity and rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2222")}, {Put, "c", []byte("3")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestDeleteThenPutSameBatch(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || r.Revision != 2 {
		t.Fatal(e, r)
	}
	rec, ok, _ := s.Get("a")
	if !ok || string(rec.Value) != "2" || rec.Revision != 2 {
		t.Fatal(rec)
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
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*20 || snap.NextRevision != 32*20+1 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}
