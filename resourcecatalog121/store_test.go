package resourcecatalog121

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameAndValueBounds(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "UPPER", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "这个", []byte("v")},
		{Put, "waytoolongname", []byte("v")},
		{Put, "ok", make([]byte, 9)},
		{Kind(0), "ok", nil},
		{Kind(99), "ok", nil},
	}
	for _, op := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a-b_c9", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
}

func TestRevisionGapsAndNextRevision(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 || len(x.Changed) != 1 || x.Changed[0].Name != "b" || x.Changed[0].Revision != 2 {
		t.Fatal(x)
	}
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
	// Failed batch must not consume revisions.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}, {Delete, "zz", nil}}})
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("x")}}})
	if x.Revision != 3 || s.Snapshot().NextRevision != 4 {
		t.Fatal(x)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	// Intermediate state exceeds record limit, final state does not.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1111")}, {Put, "b", []byte("2222")}, {Put, "c", []byte("3")}, {Delete, "c", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Final total value bytes exceeded -> rollback.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	r, _, _ := s.Get("a")
	if string(r.Value) != "1111" {
		t.Fatal(r)
	}
	// Final record count exceeded.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if !reflect.DeepEqual([]string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}, []string{"a", "b", "c"}) {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal(r)
	}
}

func TestGetInvalidAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); ok || e != nil {
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("v")}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 16 {
		t.Fatal(snap)
	}
	// Revisions strictly increase with generation count of successful batches.
	if snap.NextRevision <= snap.Generation {
		t.Fatal(snap)
	}
}
