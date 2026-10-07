package metacatalog356

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
	s, e := New(Options{MaxRecords: 8, MaxNameBytes: 3, MaxValueBytes: 2, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	ok := []string{"a", "abc", "a-_", "0", "---"}
	for _, n := range ok {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "abcd", "A", "a b", "a.b", "é", "a/b"}
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
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after invalid batches")
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Exceeds record and total-value limits mid-batch, legal at the end.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1111")},
		{Put, "b", []byte("2222")},
		{Put, "c", []byte("3")},
		{Delete, "c", nil},
		{Put, "a", []byte("1")},
		{Put, "b", []byte("2")},
	}})
	if e != nil || len(x.Changed) != 3 {
		t.Fatal(e, x)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	// Over the record limit at batch end: rollback.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "d", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
	// Over the total-value limit at batch end: rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("4444")}, {Put, "b", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRevisionAndGeneration(t *testing.T) {
	s := store(t)
	if g := s.Snapshot(); g.Generation != 0 || g.NextRevision != 1 {
		t.Fatal(g)
	}
	// Empty batch: success, generation unchanged.
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	g := s.Snapshot()
	if g.Generation != 1 || g.NextRevision != 3 {
		t.Fatal(g)
	}
	// Failed batch: generation and revision unchanged.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Delete, "zz", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(g, s.Snapshot()) {
		t.Fatal("snapshot changed after failed batch")
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1")}, {Put, "a", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	snap.Records[1].Name = "zzz"
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "2" {
		t.Fatal(r, ok, e)
	}
	if _, ok, _ := s.Get("zzz"); ok {
		t.Fatal("mutated snapshot leaked into store")
	}
	if _, ok, e := s.Get("missing"); e != nil || ok {
		t.Fatal(ok, e)
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
			k := fmt.Sprintf("k-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				r, ok, e := s.Get(k)
				if e != nil || !ok {
					t.Error(r, ok, e)
					return
				}
				_ = s.Snapshot()
			}
			if _, e := s.Apply(Batch{Ops: []Op{{Delete, k, nil}}}); e != nil {
				t.Error(e)
			}
		}()
	}
	w.Wait()
	g := s.Snapshot()
	if len(g.Records) != 0 || g.Generation != 32*21 || g.NextRevision != 32*20+1 {
		t.Fatal(g)
	}
}
