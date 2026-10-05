package resourcecatalog101

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

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 8, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	good := []string{"a", "abc-123_x", "0", "z"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"}
	for _, n := range bad {
		b := s.Snapshot()
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if !reflect.DeepEqual(b, s.Snapshot()) {
			t.Fatalf("name %q mutated state", n)
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
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing name would be ErrNotFound, but structural error must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch exceeds record capacity, but final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "a", nil},
	}})
	if e != nil || len(s.Snapshot().Records) != 2 || x.Generation != 1 {
		t.Fatal(e, x)
	}
	// Final total value bytes exceeded -> ErrCapacity with rollback.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("5")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestDeleteMissingAndRePut(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "2" || r.Revision != 2 {
		t.Fatal(r, ok)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().NextRevision != 1 {
		t.Fatal(s.Snapshot())
	}
}

func TestRevisionContinuityAndRollback(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 || x.Changed[0].Revision != 1 || x.Changed[1].Revision != 2 {
		t.Fatal(x)
	}
	// Failed batch must not consume revisions or bump generation.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Delete, "ghost", nil}}})
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Revision != 3 || x.Generation != 2 {
		t.Fatal(x)
	}
}

func TestGetErrorsAndIsolation(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal(r)
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
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 0 || snap.Generation != 32*21 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	// Revisions contiguous: 32 goroutines * 20 puts; deletes allocate none.
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}
