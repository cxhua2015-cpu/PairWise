package metacatalog201

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
	ok := []string{"a", "z09_-x", "abcdefghijkl"} // exactly 12 bytes allowed
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcdefghijklm", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestValueAndKindValidation(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Validation happens before any state read: invalid op later in the
	// batch must fail even if an earlier op would hit ErrNotFound.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(x, e)
	}
}

func TestRevisionContinuityAndDelete(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 || x.Generation != 1 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Revision != 2 || x.Generation != 2 { // delete allocates no revision
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("3")}}})
	if x.Revision != 3 {
		t.Fatal(x)
	}
	r, ok, _ := s.Get("a")
	if !ok || r.Revision != 3 || string(r.Value) != "3" {
		t.Fatal(r, ok)
	}
	if s.Snapshot().NextRevision != 4 {
		t.Fatal(s.Snapshot())
	}
}

func TestRollbackRestoresRevisionAndGeneration(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	// Puts allocate revisions 2,3 then the delete fails: nothing may leak.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state leaked after rollback")
	}
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if x.Revision != 2 || x.Generation != 2 {
		t.Fatal(x)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	// Over the limit mid-batch, back under by the end: must succeed.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("ab")}, {Put, "b", []byte("cd")}, {Put, "c", []byte("ef")}, // 3 records, 6 bytes mid-batch
		{Delete, "c", nil}, {Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Over the limit at the end: must fail and roll back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1234")}, {Put, "y", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 1 {
		t.Fatal(s.Snapshot())
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "x", []byte("1")}, {Put, "y", []byte("2")}, {Put, "z", []byte("3")}}})
	if !errors.Is(e, ErrCapacity) { // MaxRecords = 2
		t.Fatal(e)
	}
}

func TestGetNotFoundAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestChangedDedupAndOrder(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Delete, "b", nil}, {Put, "b", []byte("3")},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	if x.Changed[0].Name != "a" || x.Changed[1].Name != "b" || string(x.Changed[1].Value) != "3" {
		t.Fatal(x.Changed)
	}
	// Mutating Changed must not affect the store.
	x.Changed[1].Value[0] = 'q'
	r, _, _ := s.Get("b")
	if string(r.Value) != "3" {
		t.Fatal("Changed aliases internal state")
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
			k := fmt.Sprintf("k%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	seen := map[uint64]bool{}
	for _, r := range snap.Records {
		if seen[r.Revision] {
			t.Fatal("duplicate revision", r.Revision)
		}
		seen[r.Revision] = true
		if r.Revision >= snap.NextRevision {
			t.Fatal("revision out of range", r, snap.NextRevision)
		}
	}
}
