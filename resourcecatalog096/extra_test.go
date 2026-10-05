package resourcecatalog096

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{}, {MaxRecords: 1}, {MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s := store(t)
	for _, n := range []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok_name-1", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestUnknownKindAndValueLimit(t *testing.T) {
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
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil || s.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(r, e)
	}
	if s.Snapshot().NextRevision != 3 {
		t.Fatal(s.Snapshot())
	}
}

func TestDeleteKeepsRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	r, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Revision != 1 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 2 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestCapacityRollbackRevision(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}})
	b := s.Snapshot()
	// Transiently within limits, over only at batch end.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("12345678")}, // total 24 > 16 at end
	}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
	// Record-count overflow also rolls back revision.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "c", []byte("1")}, {Put, "d", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e, s.Snapshot())
	}
}

func TestCapacityCheckedOnlyAtEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("zzzz")}}})
	// Over capacity mid-batch (a+b), back within limits after deleting a.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("yy")}, {Delete, "a", nil}}}); e != nil {
		t.Fatal(e)
	}
	if _, ok, _ := s.Get("b"); !ok {
		t.Fatal("b missing")
	}
}

func TestChangedReflectsFinalState(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "a", []byte("2")}, {Delete, "a", nil}, {Put, "a", []byte("3")}}})
	if e != nil || len(r.Changed) != 1 || string(r.Changed[0].Value) != "3" || r.Changed[0].Revision != 3 {
		t.Fatal(r, e)
	}
	// Put then Delete: name absent from Changed.
	r, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Delete, "b", nil}}})
	if e != nil || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%03d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 672 || snap.NextRevision != 641 {
		t.Fatal(snap.Generation, snap.NextRevision, len(snap.Records))
	}
}

func TestConcurrentRevisionUnique(t *testing.T) {
	s, _ := New(Options{MaxRecords: 256, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 4096})
	const n = 16
	var w sync.WaitGroup
	revs := make([][]uint64, n)
	for i := 0; i < n; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%02d", i)
			for j := 0; j < 10; j++ {
				r, e := s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				if e != nil {
					t.Error(e)
					return
				}
				revs[i] = append(revs[i], r.Revision)
			}
		}()
	}
	w.Wait()
	seen := map[uint64]bool{}
	for _, rs := range revs {
		for _, r := range rs {
			if seen[r] {
				t.Fatal("duplicate revision", r)
			}
			seen[r] = true
		}
	}
	if len(seen) != 160 || s.Snapshot().NextRevision != 161 {
		t.Fatal(len(seen), s.Snapshot().NextRevision)
	}
}
