package metacatalog301

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
	if _, e := New(Options{1, 1, 1, 1}); e != nil {
		t.Fatal(e)
	}
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 12, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	valid := []string{"a", "abc-09_x", "0", "z"}
	for _, n := range valid {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q should be valid: %v", n, e)
		}
	}
	invalid := []string{"", "A", "a b", "a.b", "a/b", "é", "UPPER"}
	for _, n := range invalid {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestNameAndValueLengthLimits(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcd", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("xyz")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abc", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKind(t *testing.T) {
	s := store(t)
	for _, k := range []Kind{0, 3, 255} {
		if _, e := s.Apply(Batch{Ops: []Op{{k, "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("kind %d: %v", k, e)
		}
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Generation != 2 || x.Revision != 2 {
		t.Fatal(x)
	}
}

func TestDeleteDoesNotAllocateRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, _ := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 {
		t.Fatal(x)
	}
	r, ok, _ := s.Get("b")
	if !ok || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	// Transient overflow inside the batch is fine; only final state counts.
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("w")}, {Delete, "a", nil}}})
	if e != nil || len(s.Snapshot().Records) != 1 {
		t.Fatal(e, x)
	}
	// Final overflow fails and rolls back.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 1 {
		t.Fatal("state must be rolled back")
	}
}

func TestTotalValueBytesCapacity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Overwrite in place keeps total within budget.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRollbackRestoresRevisionAndGeneration(t *testing.T) {
	s := store(t)
	x1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "zz", nil}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("snapshot changed after failed batch")
	}
	x2, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if x2.Generation != x1.Generation+1 || x2.Revision != x1.Revision+1 {
		t.Fatal(x1, x2)
	}
}

func TestChangedSortedAndFinalState(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")},
		{Put, "c", []byte("3")}, {Delete, "a", nil}, {Put, "a", []byte("4")},
	}})
	if e != nil || len(x.Changed) != 2 {
		t.Fatal(e, x)
	}
	if x.Changed[0].Name != "a" || x.Changed[1].Name != "c" {
		t.Fatal(x.Changed)
	}
	if string(x.Changed[0].Value) != "4" || x.Changed[0].Revision != 4 {
		t.Fatal(x.Changed[0])
	}
	if string(x.Changed[1].Value) != "3" || x.Changed[1].Revision != 3 {
		t.Fatal(x.Changed[1])
	}
}

func TestDeletedNamesAbsentFromChanged(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotDeepCopyIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'X'
	r, _, _ := s.Get("a")
	if string(r.Value) != "v" {
		t.Fatal("snapshot shares memory with store")
	}
	if s.Snapshot().NextRevision != 2 {
		t.Fatal(s.Snapshot())
	}
}

func TestSnapshotSorted(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 4, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Put, "a", nil}, {Put, "b", nil}}})
	recs := s.Snapshot().Records
	if recs[0].Name != "a" || recs[1].Name != "b" || recs[2].Name != "c" {
		t.Fatal(recs)
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
			k := fmt.Sprintf("k%02d", i)
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
	// 32 goroutines * 20 successful put-batches and 20 delete-batches.
	if snap.Generation != 32*20*2 {
		t.Fatal(snap.Generation)
	}
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}

func TestConcurrentRevisionsUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	seen := make(chan uint64, 64*10)
	for i := 0; i < 64; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 10; j++ {
				x, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("n%02d", i), []byte("v")}}})
				if e == nil {
					seen <- x.Revision
				}
			}
		}()
	}
	w.Wait()
	close(seen)
	uniq := map[uint64]bool{}
	maxRev := uint64(0)
	for r := range seen {
		if uniq[r] {
			t.Fatalf("duplicate revision %d", r)
		}
		uniq[r] = true
		if r > maxRev {
			maxRev = r
		}
	}
	if maxRev != 64*10 {
		t.Fatal(maxRev)
	}
}
