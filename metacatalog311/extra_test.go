package metacatalog311

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	cases := []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, 1, 1, -5}, {0, 0, 0, 0},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b", "UPPER"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	good := []string{"a", "z09", "-_-", "a-b"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if len(s.Snapshot().Records) != len(good) {
		t.Fatal(s.Snapshot().Records)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("toolong")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if got := s.Snapshot().Generation; got != 0 {
		t.Fatal(got)
	}
}

func TestValidationPrecedesStateRead(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	// Delete of a missing key would yield ErrNotFound, but the structurally
	// invalid op later in the batch must win because validation runs first.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{Ops: nil})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
	if snap := s.Snapshot(); snap.Generation != 1 || snap.NextRevision != 2 {
		t.Fatal(snap)
	}
}

func TestRevisionContinuityAndDeleteNoAlloc(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Revision != 2 || x.Generation != 2 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("v")}}})
	if e != nil || x.Revision != 3 {
		t.Fatal(x, e)
	}
	r, ok, e := s.Get("c")
	if e != nil || !ok || r.Revision != 3 {
		t.Fatal(r, ok, e)
	}
	if snap := s.Snapshot(); snap.NextRevision != 4 {
		t.Fatal(snap)
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	s, e := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("11")}, {Put, "b", []byte("22")}}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	// Would exceed MaxRecords and MaxTotalValueBytes only at batch end.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("33")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Revision must not leak from the failed batch.
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "b", nil}, {Put, "c", []byte("33")}}})
	if e != nil || x.Revision != 3 {
		t.Fatal(x, e)
	}
	r, ok, e := s.Get("c")
	if e != nil || !ok || r.Revision != 3 {
		t.Fatal(r, ok, e)
	}
}

func TestTotalValueBytesBoundary(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}}); e != nil {
		t.Fatal(e)
	}
	// Overwrite with smaller values first, then grow: final total matters.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("234")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}, {Put, "c", []byte("5")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestChangedDedupSorted(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if e != nil {
		t.Fatal(e)
	}
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}, {Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.Changed) != 1 || x.Changed[0].Name != "b" || x.Changed[0].Revision != 3 || string(x.Changed[0].Value) != "3" {
		t.Fatal(x.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	snap.Records[0].Value[0] = 'X'
	snap.Records[1].Name = "zzz"
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "2" {
		t.Fatal(r, ok, e)
	}
	if _, ok, _ = s.Get("zzz"); ok {
		t.Fatal("snapshot mutation leaked into store")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				if _, err := s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}}); err != nil {
					t.Error(err)
					return
				}
				if _, ok, err := s.Get(k); err != nil || (j > 0 && !ok) {
					t.Error(err, ok)
					return
				}
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*20 || snap.NextRevision != 32*20+1 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("snapshot not sorted")
		}
	}
}

func TestConcurrentPutDelete(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", []byte("v")}, {Delete, "k", nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 {
		t.Fatal(snap.Records)
	}
	// Each successful batch bumps generation once and allocates one revision.
	if snap.Generation == 0 || snap.NextRevision != snap.Generation+1 {
		t.Fatal(snap.Generation, snap.NextRevision)
	}
}
