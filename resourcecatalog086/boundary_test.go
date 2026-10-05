package resourcecatalog086

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

func TestNameAndValueValidation(t *testing.T) {
	s := store(t)
	bad := []Op{
		{Put, "", []byte("v")},
		{Put, "Bad", []byte("v")},
		{Put, "has space", []byte("v")},
		{Put, "dot.name", []byte("v")},
		{Put, "é", []byte("v")},
		{Put, "toolongname13c", []byte("v")}, // > MaxNameBytes(12)
		{Put, "ok", []byte("123456789")},     // > MaxValueBytes(8)
		{Delete, "ok", []byte("x")},          // Delete must not carry a value
		{Kind(0), "ok", nil},
		{Kind(3), "ok", nil},
	}
	for _, op := range bad {
		b := s.Snapshot()
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
		if !reflect.DeepEqual(b, s.Snapshot()) {
			t.Fatalf("op %+v mutated state", op)
		}
	}
	// Boundary-valid names and values are accepted.
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "a-z_0-9-----", []byte("12345678")},
	}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestDeleteNoRevisionAllocated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Revision != 1 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	y, _ := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if y.Revision != 2 {
		t.Fatal(y.Revision)
	}
	if snap := s.Snapshot(); snap.NextRevision != 3 || snap.Generation != 3 {
		t.Fatal(snap)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Transiently exceeds both limits within the batch, fine at the end.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")},
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("12345678")}, // 3 records, 24 bytes mid-batch
		{Delete, "a", nil},
		{Delete, "c", nil},
	}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "b" {
		t.Fatal(e, x)
	}
	// Exceeding at the end fails and rolls back.
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("12345678")},
		{Put, "d", []byte("1")},
	}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("capacity failure must roll back")
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "xy" {
		t.Fatal(r, ok, e)
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
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte{byte(j)}}}})
				if r, ok, e := s.Get(k); e != nil || (ok && len(r.Value) != 1) {
					t.Error(r, ok, e)
					return
				}
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			if _, ok, _ := s.Get(k); ok {
				t.Error("deleted key still visible")
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 32*21 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
}

func TestConcurrentBatchAtomicity(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 4})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%d", i%4)
			// Half the batches fail on capacity; state must stay consistent.
			_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k + "x", []byte("w")}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	total := 0
	for _, r := range snap.Records {
		total += len(r.Value)
	}
	if len(snap.Records) > 2 || total > 4 {
		t.Fatal("capacity invariant violated", snap)
	}
}
