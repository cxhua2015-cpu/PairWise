package resourcecatalog111

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
		}
	}
}

func TestNameCharset(t *testing.T) {
	s, _ := New(Options{MaxRecords: 16, MaxNameBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 128})
	valid := []string{"a", "abc-xyz_09", "0", "-", "_"}
	for _, n := range valid {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); e != nil {
			t.Fatalf("name %q should be valid: %v", n, e)
		}
	}
	invalid := []string{"", "A", "a b", "a.b", "é", "a/b", "\x7f"}
	for _, n := range invalid {
		if _, e := s.Apply(Batch{Ops: []Op{{Put, n, []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", n, e)
		}
	}
}

func TestNameAndValueLengthLimits(t *testing.T) {
	s := store(t) // MaxNameBytes 12, MaxValueBytes 8
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijkl", []byte("12345678")}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "abcdefghijklm", []byte("v")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "ok", []byte("123456789")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestUnknownKindAndDeleteValue(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(0), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind(99), "a", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", []byte("x")}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeStateRead(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("x")}}})
	b := s.Snapshot()
	// Second op is structurally invalid; first op must not be applied.
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("y")}, {Kind(7), "c", nil}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestFinalCapacityCheckedAtBatchEnd(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}, {Put, "b", []byte("34")}}})
	// Mid-batch the store would exceed both limits, but final state fits.
	x, e := s.Apply(Batch{Ops: []Op{
		{Put, "c", []byte("56")},
		{Put, "a", []byte("7890")},
		{Delete, "b", nil},
		{Delete, "c", nil},
	}})
	if e != nil || len(x.Changed) != 1 || x.Changed[0].Name != "a" {
		t.Fatal(e, x)
	}
	r, _, _ := s.Get("a")
	if string(r.Value) != "7890" {
		t.Fatal(string(r.Value))
	}
}

func TestCapacityExceededRollback(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("12")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34")}, {Put, "c", []byte("56")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
	_, e = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("34567")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}

func TestGenerationAndRevisionCounters(t *testing.T) {
	s := store(t)
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	// Empty batch: success, generation unchanged.
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if g := s.Snapshot().Generation; g != 0 {
		t.Fatal(g)
	}
	// Failed batch: generation and revision unchanged.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}}})
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "b", []byte("2")}, {Delete, "zz", nil}}})
	snap := s.Snapshot()
	if snap.Generation != 1 || snap.NextRevision != 2 {
		t.Fatal(snap)
	}
	// Delete-only batch: generation bumps, revision does not advance.
	x, e = s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if e != nil || x.Generation != 2 || x.Revision != 1 || len(x.Changed) != 0 {
		t.Fatal(e, x)
	}
	if s.Snapshot().NextRevision != 2 {
		t.Fatal(s.Snapshot().NextRevision)
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := store(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "ghost", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}}})
	if _, e := s.Apply(Batch{Ops: []Op{{Delete, "a", nil}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGetInvalidNameAndMissing(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	snap := s.Snapshot()
	snap.Records[0].Value[0] = 'Z'
	r, _, _ := s.Get("a")
	if string(r.Value) != "xy" {
		t.Fatal(string(r.Value))
	}
	// Snapshot ordering by name.
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("1")}, {Put, "b", []byte("2")}}})
	snap = s.Snapshot()
	names := []string{snap.Records[0].Name, snap.Records[1].Name, snap.Records[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatal(names)
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
			k := fmt.Sprintf("key-%02d", i)
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
	if len(snap.Records) != 0 {
		t.Fatal(len(snap.Records))
	}
	if snap.Generation != 32*20*2 {
		t.Fatal(snap.Generation)
	}
}
