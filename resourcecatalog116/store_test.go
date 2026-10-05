package resourcecatalog116

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
		{0, 1, 1, 1}, {1, 0, 1, 1}, {1, 1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 1, 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestNameBoundaries(t *testing.T) {
	s, e := New(Options{MaxRecords: 4, MaxNameBytes: 2, MaxValueBytes: 2, MaxTotalValueBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"", "abc", "A", "a b", "a.b", "é"} {
		if _, e = s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z0", "-_", "9"} {
		if _, e = s.Apply(Batch{Ops: []Op{{Put, n, nil}}}); e != nil {
			t.Fatalf("name %q: %v", n, e)
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
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", bytes.Repeat([]byte("x"), 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	// Second op is structurally invalid; first op would fail with ErrNotFound
	// if state were read before full validation. Must report ErrInvalidInput.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad?", nil}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	// Intermediate state has 2 records / 6 bytes (over total), final is fine.
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("123")},
		{Put, "b", []byte("456")},
		{Put, "a", []byte("7")},
	}})
	if e != nil || r.Revision != 3 {
		t.Fatal(e, r)
	}
	// Final state exceeds record capacity: whole batch rolls back.
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("8")}, {Put, "d", []byte("9")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	// Final state exceeds total value bytes.
	_, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}, {Put, "c", []byte("4444")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestRevisionAndGenerationSemantics(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if e != nil || r.Generation != 1 || r.Revision != 1 {
		t.Fatal(e, r)
	}
	// Delete does not allocate a revision.
	r, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Put, "c", []byte("3")}, {Delete, "b", nil}}})
	if e != nil || r.Generation != 2 || r.Revision != 3 {
		t.Fatal(e, r)
	}
	snap := s.Snapshot()
	if snap.Generation != 2 || snap.NextRevision != 4 || len(snap.Records) != 1 || snap.Records[0].Name != "c" {
		t.Fatal(snap)
	}
	// Empty batch: generation unchanged.
	r, e = s.Apply(Batch{})
	if e != nil || r.Generation != 2 || r.Revision != 3 || len(r.Changed) != 0 {
		t.Fatal(e, r)
	}
	// Failed batch: generation and revision unchanged.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "d", []byte("4")}, {Delete, "zz", nil}}})
	if !reflect.DeepEqual(s.Snapshot(), snap) {
		t.Fatal("state changed after failed batch")
	}
}

func TestChangedContents(t *testing.T) {
	s := store(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Put, "b", []byte("1")},
		{Put, "a", []byte("2")},
		{Put, "b", []byte("3")},
		{Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 2 || r.Changed[0].Name != "a" || r.Changed[1].Name != "b" {
		t.Fatal(r.Changed)
	}
	if r.Changed[1].Revision != 3 || string(r.Changed[1].Value) != "3" {
		t.Fatal(r.Changed[1])
	}
}

func TestGetMissingAndInvalid(t *testing.T) {
	s := store(t)
	if _, ok, e := s.Get("nope"); ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'q'
	r, ok, _ := s.Get("a")
	if !ok || string(r.Value) != "xy" {
		t.Fatal(r)
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
			k := fmt.Sprintf("k%03d", i)
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
		t.Fatal(snap.Generation, len(snap.Records))
	}
	// Revisions are contiguous: 32 goroutines * 20 puts each (deletes allocate none).
	if snap.NextRevision != 32*20+1 {
		t.Fatal(snap.NextRevision)
	}
}
