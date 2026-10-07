package metacatalog386

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	valid := Options{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Options{
		{}, {MaxRecords: 1}, {MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s := store(t)
	for _, name := range []string{"", "A", "a b", "a.b", "a/b", "é", "aB", "toolongname123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", name, e)
		}
		if _, _, e := s.Get(name); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", name, e)
		}
	}
	for _, name := range []string{"a", "z-0_9", "abc-def_123"} {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, name, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", name, e)
		}
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	b := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 0, Name: "a", Value: []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestValueTooLarge(t *testing.T) {
	s := store(t) // MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 8)}}}); e != nil {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but the later invalid
	// op must win because structural validation happens first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("v")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
	r, e = s.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestGenerationBumpsOncePerBatch(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Put, "b", []byte("v")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 || r.Revision != 2 {
		t.Fatal(e, r)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := store(t) // MaxRecords 3, MaxTotalValueBytes 16
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12345678")}, {Put, "b", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	b := s.Snapshot()
	// Total value bytes 24 > 16 at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12345678")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Record count 4 > 3 at batch end.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "d", []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestMidBatchCapacityOverflowAllowed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Mid-batch total is 6 > 4, but the batch ends within limits.
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("123")}, {Put, "b", []byte("123")}, {Delete, "a", nil}, {Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionRollback(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("v")}, {Delete, "missing", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	r, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("v")}}})
	if e != nil || r.Revision != 1 {
		t.Fatal(e, r)
	}
}

func TestChangedSortedAndFinal(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "a", []byte("2")}, {Put, "b", []byte("3")}, {Delete, "c", nil}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r.Changed)
	}
	if r.Changed[0].Revision != 2 || r.Changed[1].Revision != 3 {
		t.Fatal(r.Changed)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" {
		t.Fatal(snap.Records)
	}
	if snap.Generation != 1 || snap.NextRevision != 3 {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'x'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "1" {
		t.Fatal(e, ok, r)
	}
}

func TestGetMissing(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(e, ok)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 16, MaxTotalValueBytes: 4096})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Put, k, []byte("w")}, {Delete, k, nil}, {Put, k, []byte("z")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	// 3 puts per iteration, 50 iterations, 32 goroutines.
	if want := uint64(3 * 50 * 32); snap.NextRevision != want+1 {
		t.Fatal(snap.NextRevision, want)
	}
	if snap.Generation != uint64(50*32) {
		t.Fatal(snap.Generation)
	}
}
