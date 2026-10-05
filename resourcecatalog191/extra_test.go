package resourcecatalog191

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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	s := store(t)
	// Unknown kind and bad name must fail even though "z" does not exist.
	for _, b := range []Batch{
		{Ops: []Op{{Kind: 0, Name: "a"}}},
		{Ops: []Op{{Kind: 99, Name: "a"}}},
		{Ops: []Op{{Delete, "BAD", nil}}},
		{Ops: []Op{{Put, "", []byte("x")}}},
		{Ops: []Op{{Put, "this-name-is-way-too-long", []byte("x")}}},
		{Ops: []Op{{Put, "a", []byte("012345678")}}}, // > MaxValueBytes
	} {
		if _, e := s.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: got %v", b, e)
		}
	}
	// Structural error later in the batch must win over earlier state errors.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Kind: 7, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	ok := []string{"a", "z", "0", "9", "-", "_", "a-b_c9"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"A", "a b", "a.b", "é", "a/b", "\x7f"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
	r, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch must not bump generation.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if g := s.Snapshot().Generation; g != 1 {
		t.Fatal(g)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	r, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if s.Snapshot().NextRevision != 2 {
		t.Fatal(s.Snapshot())
	}
}

func TestFinalCapacityCheckedAtEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch there are 3 records and 6 bytes, but the final state fits.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "c", nil},
	}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "b" {
		t.Fatal(r, e)
	}
	// Final state over capacity must fail and roll back.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("333")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRollbackRestoresRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || r.Revision != 2 { // revision 2 reused, not skipped
		t.Fatal(r, e)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("BAD"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("ok-name"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
	}
	// Mutating caller's input after Apply must not affect stored value.
	v := []byte("zz")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "d", v}}})
	v[0] = 'q'
	r, _, _ = s.Get("d")
	if !bytes.Equal(r.Value, []byte("zz")) {
		t.Fatal(r.Value)
	}
}

func TestChangedSortedFinalState(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Delete, "b", nil},
		{Put, "c", []byte("3")},
	}})
	if e != nil || len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "c" {
		t.Fatal(r, e)
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
			k := fmt.Sprintf("k-%02d", i%16)
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
	if snap.Generation == 0 {
		t.Fatal("no successful batches")
	}
	for _, r := range snap.Records {
		if r.Revision == 0 || r.Revision >= snap.NextRevision {
			t.Fatal(r, snap.NextRevision)
		}
	}
}

func TestConcurrentRevisionsUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	var mu sync.Mutex
	seen := map[uint64]string{}
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("w%d", i)
			for j := 0; j < 25; j++ {
				r, e := s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				if e != nil {
					continue
				}
				mu.Lock()
				if prev, dup := seen[r.Revision]; dup {
					t.Errorf("revision %d reused by %s and %s", r.Revision, prev, k)
				}
				seen[r.Revision] = k
				mu.Unlock()
			}
		}()
	}
	w.Wait()
}
