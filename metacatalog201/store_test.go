package metacatalog201

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

func TestNameAndValueLimits(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Upper", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "toolongname12", []byte("v")},
		{Put, "ok", []byte("123456789")},
		{Kind(0), "ok", []byte("v")},
		{Kind(99), "ok", []byte("v")},
	}
	for _, op := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a-b_c9", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || r.Generation != 2 || r.Revision != 2 {
		t.Fatal(e, r)
	}
}

func TestDeleteMissingAndCapacityRollback(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}, {Put, "c", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 0 {
		t.Fatal(n)
	}
	if s.Snapshot().NextRevision != 1 {
		t.Fatal("revision leaked after rollback")
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap.Records)
	}
	snap.Records[0].Value[0] = 'x'
	r, _, _ := s.Get("a")
	if string(r.Value) != "2" {
		t.Fatal("snapshot aliases internal state")
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
	if snap.Generation != 32*20*2 {
		t.Fatalf("generation=%d", snap.Generation)
	}
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	if snap.NextRevision != 32*20+1 {
		t.Fatalf("nextRev=%d", snap.NextRevision)
	}
}

func TestChangedReflectsFinalState(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "a" || string(r.Changed[0].Value) != "2" || r.Changed[0].Revision != 2 {
		t.Fatal(e, r)
	}
	r, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Name != "a" || r.Changed[0].Value != nil {
		t.Fatal(e, r)
	}
	if !reflect.DeepEqual(s.Snapshot().Records, []Record{}) {
		t.Fatal(s.Snapshot())
	}
}
