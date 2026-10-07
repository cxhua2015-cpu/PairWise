package metacatalog351

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

func TestNameBoundaries(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	good := []string{"a", "abc", "a-1", "_", "0-_"}

	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcd", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("too-long name accepted")
	}
	for _, n := range []string{"", "A", "a b", "a.b", "é"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("bad name %q accepted", n)
		}
	}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("good name %q rejected: %v", n, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueTooLarge(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	// Delete of a missing key would be ErrNotFound, but the structural
	// error later in the batch must win because validation comes first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch bumped generation")
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state has 2 records / 6 bytes, but final state fits.
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xyz")}, {Put, "b", []byte("xyz")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Final state exceeds capacity: rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s.Snapshot().Generation != 1 || len(s.Snapshot().Records) != 1 {
		t.Fatal("capacity failure leaked state")
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 {
		t.Fatal(r1)
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r2.Revision != 2 || r2.Generation != 2 || len(r2.Changed) != 0 {
		t.Fatal(r2)
	}
	r3, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if r3.Revision != 3 || r3.Changed[0].Revision != 3 {
		t.Fatal(r3)
	}
	if got := s.Snapshot().NextRevision; got != 4 {
		t.Fatal(got)
	}
}

func TestRollbackRestoresRevision(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("rollback changed state")
	}
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if r.Changed[0].Revision != 2 {
		t.Fatal("revision leaked from failed batch", r)
	}
}

func TestChangedDedupAndSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "z", []byte("1")}, {Put, "m", []byte("2")}, {Put, "z", []byte("3")}, {Delete, "z", nil},
	}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "m" {
		t.Fatal(r, e)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "c", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
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
			k := fmt.Sprintf("key-%02d", i)
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
	// Revisions are never reused: 32 keys * 20 puts = 640 allocations.
	if snap.NextRevision != 641 {
		t.Fatal(snap.NextRevision)
	}
}
