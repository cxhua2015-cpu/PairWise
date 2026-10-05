package modelcatalog

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0},
		{-1, 1, 1, 1}, {1, -2, 1, 1}, {1, 1, -3, 1}, {1, 1, 1, -4},
	} {
		if s, e := New(o); !errors.Is(e, ErrInvalidOptions) || s != nil {
			t.Fatalf("opts %+v: got %v %v", o, s, e)
		}
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 || len(x.Changed) != 0 {
		t.Fatal(x, e)
	}
	snap := s.Snapshot()
	if snap.Generation != 0 || snap.NextRevision != 1 || len(snap.Records) != 0 {
		t.Fatal(snap)
	}
}

func TestStructuralValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// First op would fail at state-read time (delete of missing name),
	// but the later structural error must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Unknown kind.
	_, e = s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Oversized value and oversized/empty/uppercase names.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("x"), 9)}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	for _, n := range []string{"", "Upper", "with space", "dot.name", "toolongname12345"} {
		if _, e = s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
		if _, _, e = s.Get(n); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("get %q: %v", n, e)
		}
	}
	// Valid charset: lowercase, digits, hyphen, underscore.
	if _, e = s.Apply(Batch{Ops: []Op{{Put, "ok-name_1", []byte("v")}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRevisionNotAllocatedOnFailure(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}}); e != nil {
		t.Fatal(e)
	}
	// Failing batch: capacity exceeded at end; revision must roll back.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Put, "d", []byte("4")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); snap.NextRevision != 2 || snap.Generation != 1 || len(snap.Records) != 1 {
		t.Fatal(snap)
	}
	// Next successful Put gets revision 2, not 5.
	x, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}}})
	if e != nil || x.Revision != 2 {
		t.Fatal(x, e)
	}
}

func TestTotalValueBytesCheckedAtBatchEnd(t *testing.T) {
	s := store(t) // MaxTotalValueBytes 16
	// Transiently exceeds total capacity but ends within it via overwrite.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("12345678")}, // 8
		{Put, "b", []byte("12345678")}, // 16
		{Put, "a", []byte("1")},        // 9 total at end
	}})
	if e != nil || x.Revision != 3 {
		t.Fatal(x, e)
	}
	// Ending over the total fails.
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("12345678")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestChangedSortedAndDeleteEntry(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Put, "b", []byte("2")}}}); e != nil {
		t.Fatal(e)
	}
	x, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("3")}}})
	if e != nil {
		t.Fatal(e)
	}
	want := []Record{{Name: "a"}, {Name: "c", Value: []byte("3"), Revision: 3}}
	if !reflect.DeepEqual(x.Changed, want) {
		t.Fatalf("got %+v want %+v", x.Changed, want)
	}
	if x.Generation != 2 {
		t.Fatal(x.Generation)
	}
}

func TestGetMissingAndSnapshotIsolation(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}}); e != nil {
		t.Fatal(e)
	}
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Q'
	r, ok, e := s.Get("a")
	if e != nil || !ok || string(r.Value) != "xy" {
		t.Fatal(r, ok, e)
	}
	// Snapshot ordering by name.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", nil}, {Put, "b", nil}}})
	names := []string{}
	for _, rec := range s.Snapshot().Records {
		names = append(names, rec.Name)
	}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, e := New(Options{MaxRecords: 128, MaxNameBytes: 16, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := fmt.Sprintf("key-%02d", i%16)
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Delete, k, nil}}})
			}
		}()
	}
	w.Wait()
	snap := s.Snapshot()
	if snap.Generation == 0 || snap.NextRevision <= 1 {
		t.Fatal(snap)
	}
}
