package metacatalog371

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
		{-1, 1, 1, 1}, {1, 1, 1, -5},
	}
	for _, o := range cases {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "名字", "toolongnameoverflow"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: got %v", n, e)
		}
	}
	good := []string{"a", "z0-_", "012345678901"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing key would fail with ErrNotFound, but the structural
	// error later in the batch must win because validation runs first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if snap := s.Snapshot(); snap.Generation != 0 || snap.NextRevision != 1 {
		t.Fatal(snap)
	}
}

func TestRevisionSequenceAndDeleteGap(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if r1.Revision != 2 || r1.Generation != 1 {
		t.Fatal(r1)
	}
	r2, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if r2.Revision != 3 || r2.Generation != 2 || len(r2.Changed) != 1 || r2.Changed[0].Name != "c" {
		t.Fatal(r2)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 4 || len(snap.Records) != 2 {
		t.Fatal(snap)
	}
	if _, ok, _ := s.Get("a"); ok {
		t.Fatal("a should be deleted")
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Mid-batch the store holds 2 records / 4 bytes; final state is 1 record.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("11")}, {Put, "b", []byte("22")}, {Delete, "a", nil},
	}})
	if e != nil || len(r.Changed) != 1 {
		t.Fatal(e, r)
	}
	// Exceeding total bytes at the end fails and rolls back.
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3333")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestMaxRecordsRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestChangedSortedAndSnapshotSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 32})
	r, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	if e != nil {
		t.Fatal(e)
	}
	names := []string{r.Changed[0].Name, r.Changed[1].Name, r.Changed[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	recs := s.Snapshot().Records
	if recs[0].Name != "a" || recs[1].Name != "b" || recs[2].Name != "c" {
		t.Fatal(recs)
	}
}

func TestGetSnapshotIsolation(t *testing.T) {
	s := store(t)
	in := []byte("abc")
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "k", in}}})
	in[0] = 'X'
	r, ok, _ := s.Get("k")
	if !ok || string(r.Value) != "abc" {
		t.Fatal("input mutation leaked into store")
	}
	r.Value[0] = 'Y'
	snap := s.Snapshot()
	snap.Records[0].Value[1] = 'Z'
	r2, _, _ := s.Get("k")
	if string(r2.Value) != "abc" {
		t.Fatal("returned value aliases internal state")
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("z")}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// Revisions are unique and dense: 32 keys * 21 puts = 672.
	if snap.NextRevision != 673 {
		t.Fatal(snap.NextRevision)
	}
}

func TestConcurrentRevisionUniqueness(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 4096})
	const workers = 16
	revs := make([][]uint64, workers)
	var w sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 10; j++ {
				k := fmt.Sprintf("w%02d-%02d", i, j)
				r, e := s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				if e != nil {
					t.Error(e)
					return
				}
				revs[i] = append(revs[i], r.Changed[0].Revision)
			}
		}()
	}
	w.Wait()
	seen := map[uint64]bool{}
	for _, rs := range revs {
		for _, rev := range rs {
			if seen[rev] {
				t.Fatalf("duplicate revision %d", rev)
			}
			seen[rev] = true
		}
	}
	if len(seen) != workers*10 {
		t.Fatal(len(seen))
	}
}
