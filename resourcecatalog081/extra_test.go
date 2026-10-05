package resourcecatalog081

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
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatal(r, e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	ok := []string{"a", "abc", "a-1", "z_9", "000"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q should be valid: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
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
	// Second op is structurally invalid; first op would fail with
	// ErrNotFound if state were read before full validation.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
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

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Intermediate state exceeds MaxRecords, but the batch ends within limits.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 12})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("v")}, {Put, "b", []byte("v")}, {Put, "c", []byte("v")}, {Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Total value bytes exceed limit only at the end -> ErrCapacity.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Failed batch rolled back: old values intact.
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "v" {
		t.Fatal(r, ok)
	}
}

func TestRevisionContinuityAndDelete(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1)
	}
	// Delete allocates no revision.
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r2.Revision != 2 || r2.Generation != 2 || len(r2.Changed) != 0 {
		t.Fatal(r2)
	}
	r3, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r3.Revision != 3 {
		t.Fatal(r3)
	}
	if snap := s.Snapshot(); snap.NextRevision != 4 {
		t.Fatal(snap)
	}
}

func TestDeleteNotFoundRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	// Mutating the snapshot must not affect the store.
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	// 32 keys * 20 puts each succeeded; deletes allocate nothing.
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
	if snap.Generation != 32*21 {
		t.Fatal(snap.Generation)
	}
}
