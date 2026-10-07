package metacatalog336

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s := store(t)
	good := []string{"a", "z09-_", "abc-def_123"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
}

func TestStructuralBeforeState(t *testing.T) {
	s := store(t)
	// Delete of missing name would fail at state read, but the unknown
	// kind later in the batch must fail structural validation first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Kind(99), "x", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValueByteLimit(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityOnlyAtEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	// Intermediate state exceeds both limits, final state does not.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1234")}, {Put, "b", []byte("1234")}, {Put, "c", []byte("1234")},
		{Delete, "a", nil}, {Delete, "b", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 1 {
		t.Fatal(e, x)
	}
	// Final record count exceeded.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("1")}, {Put, "e", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final total value bytes exceeded.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestRevisionNotAllocatedOnFailureOrDelete(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}) // rev 1
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}})   // fails, no revision
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}}}, )
	if x.Revision != 2 {
		t.Fatal(x.Revision)
	}
	y, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}) // delete allocates nothing
	if y.Revision != 2 {
		t.Fatal(y.Revision)
	}
	z, _ := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}}})
	if z.Revision != 3 || s.Snapshot().NextRevision != 4 {
		t.Fatal(z.Revision)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	s := store(t)
	r0, e := s.Apply(Batch{})
	if e != nil || r0.Generation != 0 || len(r0.Changed) != 0 {
		t.Fatal(e, r0)
	}
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}}})
	if r1.Generation != 1 {
		t.Fatal(r1.Generation)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}})
	if s.Snapshot().Generation != 1 {
		t.Fatal(s.Snapshot().Generation)
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if r2.Generation != 2 {
		t.Fatal(r2.Generation)
	}
}

func TestChangedTombstoneAndSorted(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "b", nil}, {Put, "c", []byte("w")}}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	if x.Changed[0].Name != "b" || x.Changed[0].Revision != 0 || x.Changed[0].Value != nil {
		t.Fatalf("tombstone: %+v", x.Changed[0])
	}
	if x.Changed[1].Name != "c" || x.Changed[1].Revision != 3 {
		t.Fatalf("put: %+v", x.Changed[1])
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal(string(r.Value))
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 32*51 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	// Revisions must be strictly increasing and unique across puts.
	prev := make(map[string]uint64)
	s2, _ := New(Options{MaxRecords: 64, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 4096})
	var mu sync.Mutex
	seen := map[uint64]bool{}
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i)
			x, e := s2.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
			if e != nil {
				return
			}
			mu.Lock()
			if seen[x.Revision] {
				t.Error("duplicate revision", x.Revision)
			}
			seen[x.Revision] = true
			if x.Revision <= prev[k] {
				t.Error("non-increasing revision")
			}
			prev[k] = x.Revision
			mu.Unlock()
		}()
	}
	w.Wait()
	if len(seen) != 16 {
		t.Fatal(len(seen))
	}
}

func TestRollbackDeepEqual(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("12345678")}, {Put, "c", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
}
