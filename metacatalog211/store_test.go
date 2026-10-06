package metacatalog211

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{}, {MaxRecords: 1}, {MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 5, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	for _, bad := range []string{"", "A", "a b", "a.b", "abcdef", "é"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", bad, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a-1_b", nil}}}); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestUnknownKindAndValueTooLong(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(9), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	// Delete of a missing name would fail with ErrNotFound, but the invalid
	// name later in the batch must be reported first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
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
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{})
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
	if s.Snapshot().Generation != 1 {
		t.Fatal(s.Snapshot())
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	// Exceeds limits mid-batch but returns within them by the end.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}, {Put, "c", []byte("1234")},
		{Delete, "a", nil}, {Put, "b", []byte("12")}, {Put, "c", []byte("12")},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 {
		t.Fatal(r, e)
	}
	// Over capacity at end: full rollback.
	b := s.Snapshot()
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("1234")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestDeleteMissingRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Delete, "a", nil}, {Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after rollback")
	}
}

func TestChangedTombstonesAndOrder(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "b", nil}, {Put, "a", []byte("3")}}})
	if e != nil || len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r, e)
	}
	if r.Changed[1].Value != nil || r.Changed[0].Revision != 3 {
		t.Fatal(r.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if snap.NextRevision != 4 || snap.Generation != 1 {
		t.Fatal(snap)
	}
	for i, want := range []string{"a", "b", "c"} {
		if snap.Records[i].Name != want {
			t.Fatal(snap.Records)
		}
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 32, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 256})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
}

func TestConcurrentRevisionMonotonic(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	revs := make(chan uint64, 64)
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 8; j++ {
				r, e := s.Apply(Batch{Ops: []Op{{Put, "shared", []byte("v")}}})
				if e == nil {
					revs <- r.Revision
				}
			}
		}()
	}
	w.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for r := range revs {
		if seen[r] {
			t.Fatalf("duplicate revision %d", r)
		}
		seen[r] = true
	}
	if len(seen) != 64 {
		t.Fatal(len(seen))
	}
}
