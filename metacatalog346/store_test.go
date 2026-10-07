package metacatalog346

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	cases := []Options{
		{},
		{MaxRecords: 0, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: -1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	s := store(t)
	// Unknown kind must fail even though the name would also be missing.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Invalid name anywhere in the batch fails before any state read,
	// so a leading valid op must not be applied.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("v")}, {Put, "BAD", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("ok"); ok {
		t.Fatal("partial batch applied")
	}
	// Delete carrying a value is an extra-field violation.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ok", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Oversized value and oversized/empty names.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl3", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNameCharsetBoundary(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a-z_09", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"A", "a b", "a.b", "a/b", "\xe4\xb8\xad"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, bad, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", bad, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestDeleteDoesNotAllocateRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Revision != 2 || r.Generation != 2 {
		t.Fatal(r, e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 3 || len(snap.Records) != 1 || snap.Records[0].Name != "b" {
		t.Fatal(snap)
	}
	// Re-put after delete gets the next consecutive revision.
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("3")}}})
	if e != nil || r.Revision != 3 || r.Changed[0].Revision != 3 {
		t.Fatal(r, e)
	}
}

func TestCapacityCheckedOnlyAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state has 3 records and 6 total bytes, final state fits.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Put, "b", []byte("2")},
		{Put, "c", []byte("3")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 2 {
		t.Fatal(n)
	}
}

func TestCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("22")}, {Put, "c", []byte("33")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Total value bytes exceeded at batch end.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1111")}, {Put, "b", []byte("2")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after total-bytes failure")
	}
}

func TestChangedSortedAndDeduplicated(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")},
		{Put, "a", []byte("2")},
		{Put, "c", []byte("3")},
		{Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Deleted names are absent; remaining names sorted.
	if len(r.Changed) != 1 || r.Changed[0].Name != "a" {
		t.Fatal(r.Changed)
	}
}

func TestGetMissingAndOwnership(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	v := []byte("ab")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", v}}})
	v[0] = 'z' // caller mutation must not leak into the store
	r, _, _ := s.Get("k")
	if string(r.Value) != "ab" {
		t.Fatal(string(r.Value))
	}
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'z'
	r, _, _ = s.Get("k")
	if string(r.Value) != "ab" {
		t.Fatal(string(r.Value))
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
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	// 32 keys * 20 successful non-empty put batches + 20 delete batches each.
	if want := uint64(32 * 40); snap.Generation != want {
		t.Fatal(snap.Generation, want)
	}
	if snap.NextRevision != snap.Generation/2+1 {
		t.Fatal(snap.NextRevision)
	}
}
