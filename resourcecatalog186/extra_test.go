package resourcecatalog186

import (
	"errors"
	"fmt"
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

func TestNameCharset(t *testing.T) {
	s := store(t)
	good := []Op{{Put, "a-z_0-9", []byte("v")}}
	if _, e := s.Apply(Batch{Ops: good}); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname123"} {
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
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of missing name would be ErrNotFound, but structural
	// validation of the whole batch must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	// Mid-batch there are 4 records / 20 bytes, but final state fits.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345")},
		{Put, "b", []byte("12345")},
		{Put, "c", []byte("12345")},
		{Put, "d", []byte("12345")},
		{Delete, "d", nil},
		{Put, "a", []byte("1")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if n := len(s.Snapshot().Records); n != 3 {
		t.Fatal(n)
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")},
		{Put, "b", []byte("12345678")},
		{Put, "c", []byte("1")},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := s.Snapshot(); got.Generation != b.Generation || got.NextRevision != b.NextRevision || len(got.Records) != 0 {
		t.Fatal(got)
	}
	// Too many records also rolls back.
	_, e = s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Put, "b", []byte("1")},
		{Put, "c", []byte("1")}, {Put, "d", []byte("1")},
	}})
	if !errors.Is(e, ErrCapacity) || s.Snapshot().NextRevision != 1 {
		t.Fatal(e)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(r, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(r, e)
	}
}

func TestGetMissingAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal(string(r.Value))
	}
	if snap.NextRevision != 2 || snap.Generation != 1 {
		t.Fatal(snap)
	}
}

func TestDeleteThenPutSameName(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "a", []byte("2")},
	}})
	if e != nil || len(r.Changed) != 1 || r.Changed[0].Revision != 2 {
		t.Fatal(r, e)
	}
	got, ok, _ := s.Get("a")
	if !ok || string(got.Value) != "2" || got.Revision != 2 {
		t.Fatal(got, ok)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 100, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 800})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("k%03d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
			_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}, {Put, k, []byte("z")}}})
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 || snap.Generation != 32*21 {
		t.Fatal(len(snap.Records), snap.Generation)
	}
	for i := 1; i < len(snap.Records); i++ {
		if snap.Records[i-1].Name >= snap.Records[i].Name {
			t.Fatal("not sorted")
		}
	}
}
