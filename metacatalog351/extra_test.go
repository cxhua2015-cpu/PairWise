package metacatalog351

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
	}
	for _, o := range cases {
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
	// Unknown kind and Delete carrying a Value must fail structurally,
	// even when the Delete targets a missing name (would be ErrNotFound
	// if state were read first).
	for _, b := range []Batch{
		{Ops: []Op{{Kind(0), "a", nil}}},
		{Ops: []Op{{Kind(99), "a", nil}}},
		{Ops: []Op{{Delete, "missing", []byte("x")}}},
		{Ops: []Op{{Put, "ok", []byte("v")}, {Delete, "missing", []byte("x")}}},
	} {
		if _, e := s.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("want ErrInvalidInput, got %v", e)
		}
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal("generation changed on invalid batch", g)
	}
}

func TestNameCharsetAndLimits(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	bad := []string{"", "A", "a b", "a.b", "abcd", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
	good := []string{"a", "z09", "-_", "a-b"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal("oversize value: want ErrInvalidInput, got", e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	x, e = s.Apply(Batch{Ops: nil})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(e, x)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	// Transient overflow inside the batch is fine if the final state fits.
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("zz")},
		{Put, "b", []byte("zz")}, // total 4, ok
		{Put, "c", []byte("zz")}, // transient total 6, over limit
		{Delete, "c", nil},       // back to 4
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Final overflow must fail and roll back everything.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("zzzz")}, {Put, "b", []byte("zz")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Record-count limit checked at end too.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("z")}, {Put, "d", []byte("z")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if x.Revision != 3 {
		t.Fatal("delete must not allocate revision", x)
	}
	if snap := s.Snapshot(); snap.NextRevision != 4 {
		t.Fatal(snap.NextRevision)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Delete, "nope", nil}}})
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if x.Revision != 4 {
		t.Fatal("failed batch leaked a revision", x)
	}
}

func TestChangedSortedAndRollbackGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Delete, "b", nil}}})
	if e != nil || x.Generation != 1 {
		t.Fatal(e, x)
	}
	if len(x.Changed) != 2 || x.Changed[0].Name != "a" || x.Changed[1].Name != "b" {
		t.Fatal("Changed must be sorted by name", x.Changed)
	}
	if x.Changed[1].Revision != 0 || x.Changed[1].Value != nil {
		t.Fatal("deleted entry should carry no revision/value", x.Changed[1])
	}
	// Mutating the returned Changed must not affect the store.
	x.Changed[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("returned records must be isolated copies")
	}
	g := s.Snapshot().Generation
	_, _ = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}}})
	if s.Snapshot().Generation != g {
		t.Fatal("failed batch must not bump generation")
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(e, ok)
	}
	v := []byte("ab")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", v}}})
	v[0] = 'X' // caller mutation must not leak into the store
	r, ok, _ := s.Get("k")
	if !ok || string(r.Value) != "ab" {
		t.Fatal(r, ok)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Y'
	r, _, _ = s.Get("k")
	if string(r.Value) != "ab" {
		t.Fatal("snapshot must be a deep copy")
	}
}

func TestSnapshotSorted(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("1")}, {Put, "b", []byte("1")}}})
	recs := s.Snapshot().Records
	if len(recs) != 3 || recs[0].Name != "a" || recs[1].Name != "b" || recs[2].Name != "c" {
		t.Fatal(recs)
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
			k := fmt.Sprintf("k%03d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				if j%3 == 0 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
				}
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	total := 0
	for _, r := range snap.Records {
		total += len(r.Value)
	}
	if total > 1024 || len(snap.Records) > 128 {
		t.Fatal("capacity invariant violated")
	}
	if snap.Generation == 0 || snap.NextRevision == 0 {
		t.Fatal("counters should have advanced", snap)
	}
}

func TestConcurrentDisjointWriters(t *testing.T) {
	s, _ := New(Options{MaxRecords: 64, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("w%02d", i)
			for j := 0; j < 20; j++ {
				if _, e := s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}}); e != nil {
					t.Error(e)
					return
				}
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 || snap.Generation != 320 || snap.NextRevision != 321 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
}

func TestDeepCopyValueAlias(t *testing.T) {
	s := store(t)
	v := []byte("orig")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", v}}})
	if !bytes.Equal(v, []byte("orig")) {
		t.Fatal("store mutated caller slice")
	}
	copy(v, []byte("zzzz"))
	r, _, _ := s.Get("a")
	if string(r.Value) != "orig" {
		t.Fatal("store must copy input values", r.Value)
	}
}
