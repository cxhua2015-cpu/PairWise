package metacatalog396

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
	good := []string{"a", "z09-_", "abcdefghijkl"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "é", "abcdefghijklm", "a/b"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e := s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
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
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Invalid op later in the batch must win over an earlier state error.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
	x, e = s.Apply(Batch{})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatal(x, e)
	}
}

func TestDeleteNoRevision(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Revision != 1 || x.Generation != 2 {
		t.Fatal(x, e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 2 || snap.Generation != 2 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}})
	b := s.Snapshot()
	// Total value bytes exceeded only at batch end.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Record count exceeded at batch end.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("1")}, {Put, "d", []byte("2")}, {Put, "e", []byte("3")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Net-neutral batch within limits succeeds.
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("12345678")}}})
	if e != nil || x.Generation != 2 {
		t.Fatal(x, e)
	}
}

func TestChangedSortedAndFinal(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}, {Put, "a", []byte("4")},
	}})
	if e != nil || len(x.Changed) != 3 {
		t.Fatal(x, e)
	}
	names := []string{x.Changed[0].Name, x.Changed[1].Name, x.Changed[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
	if x.Changed[0].Revision != 4 || string(x.Changed[0].Value) != "4" {
		t.Fatal(x.Changed[0])
	}
	// Delete after put in same batch removes it from Changed.
	x, e = s.Apply(Batch{Ops: []Op{{Put, "z", []byte("1")}, {Delete, "z", nil}}})
	if e != nil || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "xy" {
		t.Fatal(r, ok, e)
	}
	snap2 := s.Snapshot()
	if string(snap2.Records[0].Value) != "xy" {
		t.Fatal(snap2)
	}
}

func TestGetMissing(t *testing.T) {
	s := store(t)
	_, ok, e := s.Get("nope")
	if e != nil || ok {
		t.Fatal(ok, e)
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
			k := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 50; j++ {
				if j%3 == 2 {
					_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
				} else {
					_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				}
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	var total int
	prev := ""
	for i, r := range snap.Records {
		if i > 0 && r.Name <= prev {
			t.Fatal("records not sorted")
		}
		prev = r.Name
		total += len(r.Value)
		if r.Revision >= snap.NextRevision {
			t.Fatal("revision out of range")
		}
	}
	if total > 1024 || len(snap.Records) > 128 {
		t.Fatal("capacity invariant violated")
	}
}

func TestConcurrentMonotonicRevision(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	const n = 64
	revs := make(chan uint64, n)
	var w sync.WaitGroup
	for i := 0; i < n; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			x, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}})
			if e == nil {
				revs <- x.Revision
			}
		}()
	}
	w.Wait()
	close(revs)
	seen := map[uint64]bool{}
	for r := range revs {
		if seen[r] {
			t.Fatalf("duplicate revision %d", r)
		}
		seen[r] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d revisions, want %d", len(seen), n)
	}
	if s.Snapshot().NextRevision != uint64(n)+1 {
		t.Fatal(s.Snapshot().NextRevision)
	}
}
