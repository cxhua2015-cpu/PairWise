package metacatalog306

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -1, 1, 1}, {1, 1, -1, 1}, {1, 1, 1, -1},
		{},
	} {
		if _, e := New(Options{o.MaxRecords, o.MaxNameBytes, o.MaxValueBytes, o.MaxTotalValueBytes}); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	for _, n := range []string{"", "A", "a b", "a.b", "a/b", "中文", "a?", string(make([]byte, 13))} {
		if len(n) == 13 {
			n = "aaaaaaaaaaaaa"
		}
		_, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_9", "aaaaaaaaaaaa"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	_ = b
}

func TestUnknownKindAndValueTooLong(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
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
	// Delete of a missing name would be ErrNotFound, but the structural
	// error in a later op must win because validation runs first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "zz", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("empty batch mutated state")
	}
}

func TestDeleteKeepsRevision(t *testing.T) {
	s := store(t)
	r1, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r2, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r2.Revision != r1.Revision || r2.Generation != r1.Generation+1 {
		t.Fatal(e, r1, r2)
	}
	r3, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if r3.Revision != r1.Revision+1 {
		t.Fatal(r3)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Temporarily exceed the record cap mid-batch, end within it.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")},
		{Put, "d", []byte("4")}, {Delete, "d", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// End over the record cap.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(s.Snapshot().Records) != 3 {
		t.Fatal("rollback failed")
	}
	// Total value bytes: 3 used; grow to 17 > 16.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	r, _, _ := s.Get("a")
	if string(r.Value) != "1" {
		t.Fatal("rollback failed")
	}
}

func TestDeleteMissingRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "a", nil}, {Delete, "a", nil}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal("snapshot aliases internal state")
	}
	if snap.NextRevision != 2 || snap.Generation != 1 {
		t.Fatal(snap)
	}
}

func TestChangedSortedAndFinal(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")},
		{Put, "c", []byte("4")}, {Delete, "b", nil},
	}})
	if e != nil || len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "c" {
		t.Fatal(e, r)
	}
	if r.Changed[1].Revision != 4 || string(r.Changed[1].Value) != "4" {
		t.Fatal(r.Changed[1])
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
	// 32 goroutines * 20 iterations * 2 non-empty batches.
	if snap.Generation != 32*20*2 {
		t.Fatal(snap.Generation)
	}
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}

func TestConcurrentRevisionUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 4096})
	const n = 16
	revs := make(chan uint64, n)
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			r, e := s.Apply(Batch{Ops: []Op{{Put, fmt.Sprintf("k%02d", i), []byte("v")}}})
			if e != nil {
				t.Error(e)
				return
			}
			revs <- r.Revision
		}()
	}
	w.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for r := range revs {
		if seen[r] {
			t.Fatal("duplicate revision", r)
		}
		seen[r] = true
	}
	if len(seen) != n {
		t.Fatal(len(seen))
	}
}
