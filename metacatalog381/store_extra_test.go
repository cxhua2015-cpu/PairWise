package metacatalog381

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	for _, o := range []Options{
		{}, {0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestNameValidation(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "aaaaaaaaaaaaa", "UPPER", "x?"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
	good := []string{"a", "z", "0", "9", "-", "_", "a-b_c9", "aaaaaaaaaaaa"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(3), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would yield ErrNotFound, but the structurally
	// invalid op later in the batch must win because validation comes first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", nil}, {Put, "b", nil}, {Delete, "a", nil}}})
	if e != nil || x.Generation != 1 {
		t.Fatal(x, e)
	}
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
	// Failed batch must not bump generation.
	if _, e = s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestRevisionNotAllocatedForDelete(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", nil}}})
	if x.Revision != 1 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Revision != 1 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "b", nil}}})
	if x.Revision != 2 {
		t.Fatal(x)
	}
	if sn := s.Snapshot(); sn.NextRevision != 3 {
		t.Fatal(sn)
	}
}

func TestRollbackRestoresRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	// Puts allocate revisions 2,3 then the batch fails at the end.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("12345678")},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("revision or state leaked after rollback")
	}
	// Next successful Put must reuse revision 2.
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "b", nil}}})
	if x.Revision != 2 {
		t.Fatal(x)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds both limits, final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12")},
		{Put, "b", []byte("34")},
		{Put, "c", []byte("56")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "c" {
		t.Fatal(x, e)
	}
}

func TestCapacityExceeded(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("1")},
		{Put, "c", []byte("1")}, {Put, "d", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Exact fit is allowed.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")},
	}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndGet(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("3")}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")},
	}})
	sn := s.Snapshot()
	if len(sn.Records) != 3 || sn.Records[0].Name != "a" || sn.Records[1].Name != "b" || sn.Records[2].Name != "c" {
		t.Fatal(sn)
	}
	sn.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestChangedDeepCopy(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	x.Changed[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal("result aliases internal state")
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, k, nil}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	sn := s.Snapshot()
	if len(sn.Records) != 32 {
		t.Fatal(len(sn.Records))
	}
	// Revisions must be unique and dense: 32 keys * 20 rounds * 2 puts.
	if sn.NextRevision != 32*20*2+1 {
		t.Fatal(sn.NextRevision)
	}
	seen := map[uint64]bool{}
	for _, r := range sn.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision")
		}
		seen[r.Revision] = true
	}
	if sn.Generation != 32*20 {
		t.Fatal(sn.Generation)
	}
}
