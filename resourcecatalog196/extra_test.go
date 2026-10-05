package resourcecatalog196

import (
	"bytes"
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
		{}, {MaxRecords: 1}, {MaxRecords: 1, MaxNameBytes: 1},
		{MaxRecords: 1, MaxNameBytes: 1, MaxValueBytes: 1},
		{MaxRecords: -1, MaxNameBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameCharsetAndLength(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	good := []string{"a", "abc-1_2", "0", "z9_-"}
	for _, n := range good {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "this-name-is-too-long"}
	for _, n := range bad {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
}

func TestUnknownKindAndValueTooLarge(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("x"), 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Delete of a missing name would fail with ErrNotFound, but the batch
	// contains a structurally invalid op, which must be reported first.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state exceeds the record cap, but the batch ends within it.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("1")},
		{Put, "b", []byte("2")},
		{Put, "c", []byte("3")},
		{Delete, "a", nil},
	}})
	if e != nil || len(x.Changed) != 3 {
		t.Fatal(e, x)
	}
	snap := s.Snapshot()
	if len(snap.Records) != 2 || snap.Records[0].Name != "b" || snap.Records[1].Name != "c" {
		t.Fatal(snap)
	}
	// Total value bytes exceeded at batch end -> rollback.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("1234")}, {Put, "c", []byte("5678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestRecordCapacityRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 1, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 64})
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestRevisionContinuityAndGeneration(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	if x.Generation != 1 || x.Revision != 2 {
		t.Fatal(x)
	}
	// Delete allocates no revision.
	x, _ = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if x.Generation != 2 || x.Revision != 2 {
		t.Fatal(x)
	}
	x, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}}})
	if x.Generation != 3 || x.Revision != 3 {
		t.Fatal(x)
	}
	// Failed batch bumps neither generation nor revision.
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "nope", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	if snap.Generation != 3 || snap.NextRevision != 4 {
		t.Fatal(snap)
	}
	// Empty batch leaves generation unchanged.
	x, _ = s.Apply(Batch{})
	if x.Generation != 3 || len(x.Changed) != 0 {
		t.Fatal(x)
	}
	if s.Snapshot().Generation != 3 {
		t.Fatal("empty batch changed generation")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("3")}, {Put, "a", []byte("1")}, {Put, "b", []byte("2")}}})
	snap := s.Snapshot()
	if len(snap.Records) != 3 || snap.Records[0].Name != "a" || snap.Records[1].Name != "b" || snap.Records[2].Name != "c" {
		t.Fatal(snap)
	}
	snap.Records[0].Value[0] = 'X'
	snap.Records[0].Name = "zzz"
	again := s.Snapshot()
	if again.Records[0].Name != "a" || string(again.Records[0].Value) != "1" {
		t.Fatal("snapshot shares memory with store")
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("missing"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
			name := fmt.Sprintf("key-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, name, []byte{byte(j)}}}})
				r, ok, _ := s.Get(name)
				if ok && len(r.Value) != 1 {
					t.Error("bad value length")
				}
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, name, nil}, {Put, name, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if len(snap.Records) != 32 {
		t.Fatal(len(snap.Records))
	}
	prev := ""
	for _, r := range snap.Records {
		if r.Name <= prev {
			t.Fatal("snapshot not sorted")
		}
		prev = r.Name
	}
}
