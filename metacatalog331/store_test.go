package metacatalog331

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	for i, mutate := range []func(*Options){
		func(o *Options) { o.MaxRecords = 0 },
		func(o *Options) { o.MaxNameBytes = 0 },
		func(o *Options) { o.MaxValueBytes = 0 },
		func(o *Options) { o.MaxTotalValueBytes = 0 },
		func(o *Options) { o.MaxRecords = -1 },
	} {
		o := valid
		mutate(&o)
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("case %d: got %v", i, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 64})
	if e != nil {
		t.Fatal(e)
	}
	ok := []string{"a", "abc", "a-1", "_", "0z_"}

	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation happens before state reads: an invalid op later in
	// the batch must mask an earlier Delete of a missing name.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Kind: 7, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
	if got := s.Snapshot().Generation; got != 1 {
		t.Fatal(got)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	// Mid-batch the store exceeds both limits, but the final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")},
		{Put, "b", []byte("22")},
		{Put, "c", []byte("33")},
		{Delete, "a", nil},
		{Delete, "b", nil},
	}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "c" {
		t.Fatal(x, e)
	}
	if n := len(s.Snapshot().Records); n != 1 {
		t.Fatal(n)
	}
	// Final state exceeds limits: whole batch rolls back.
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4444")}, {Put, "e", []byte("5")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestRollbackRestoresCounters(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	// Delete of a missing name fails after allocating revisions for the Puts.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("snapshot changed")
	}
	// Revision allocation continues from the pre-failure value.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || x.Revision != 2 || x.Generation != 2 {
		t.Fatal(x, e)
	}
}

func TestDeleteThenPutSameName(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Revision != 2 || string(x.Changed[0].Value) != "2" {
		t.Fatal(x, e)
	}
	r, ok, e := s.Get("a")
	if e != nil || !ok || r.Revision != 2 {
		t.Fatal(r, ok, e)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("none"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 || len(snap.Records) != 2 ||
		snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				r, ok, err := s.Get(k)
				if err == nil && ok {
					r.Value[0] = 0xff // mutate copy; must not corrupt store
				}
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("records not sorted")
		}
	}
}
