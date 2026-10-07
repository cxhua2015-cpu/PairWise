package metacatalog416

import (
	"errors"
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

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := store(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || s.Snapshot().Generation != 0 {
		t.Fatal(e, x)
	}
}

func TestStructuralBeforeState(t *testing.T) {
	s := store(t)
	// Delete of a missing name would be ErrNotFound, but the batch also has a
	// structurally invalid op; structural validation must win.
	_, e := s.Apply(Batch{Ops: []Op{{Delete, "missing", nil}, {Put, "bad!", []byte("x")}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: 99, Name: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "a", make([]byte, 9)}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Put, "Upper", nil}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestFinalCapacityOnly(t *testing.T) {
	s, _ := New(Options{MaxRecords: 2, MaxNameBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 4})
	// Intermediate state (a+b = 5 bytes) exceeds the total cap, but the final
	// state after deleting a fits, so the batch must succeed.
	_, e := s.Apply(Batch{Ops: []Op{
		{Put, "a", []byte("xx")}, {Put, "b", []byte("yyy")}, {Delete, "a", nil},
	}})
	if e != nil {
		t.Fatal(e)
	}
	z := s.Stats()
	if z.Records != 1 || z.TotalValueBytes != 3 || z.Generation != 1 || z.NextRevision != 3 {
		t.Fatalf("%+v", z)
	}
	// Final overflow must fail and roll back revision/generation.
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Put, "c", []byte("yyyy")}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(s.Snapshot(), before) {
		t.Fatal(e)
	}
}

func TestRevisionContinuesAfterDelete(t *testing.T) {
	s := store(t)
	x, _ := s.Apply(Batch{Ops: []Op{{Put, "a", []byte("1")}, {Delete, "a", nil}, {Put, "b", []byte("2")}}})
	if x.Revision != 2 || len(x.Changed) != 1 || x.Changed[0].Name != "b" || x.Changed[0].Revision != 2 {
		t.Fatal(x)
	}
}

func TestGetValidationAndMiss(t *testing.T) {
	s := store(t)
	if _, _, e := s.Get("bad!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, ok, e := s.Get("nope"); e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestCloneIsolationAndClocks(t *testing.T) {
	s := store(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Put, "a", []byte("xy")}}})
	c, e := s.Clone()
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(c.Snapshot(), s.Snapshot()) {
		t.Fatal("clone diverges")
	}
	r, _, _ := c.Get("a")
	r.Value[0] = 'q'
	if r2, _, _ := s.Get("a"); string(r2.Value) != "xy" {
		t.Fatal("clone aliases value")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Delete, "a", nil}}})
	if s.Stats().Records != 1 || c.Stats().Records != 0 {
		t.Fatal("clone shares state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxRecords: 128, MaxNameBytes: 12, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a'+i%8)) + string(rune('0'+i/8))
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}}})
				_, _, _ = s.Get(k)
				_ = s.Snapshot()
				_ = s.Stats()
				_ = s.ValidateBatch(Batch{Ops: []Op{{Delete, k, nil}}})
				_, _ = s.Clone()
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 16 || z.Generation != 320 {
		t.Fatalf("%+v", z)
	}
}

func TestConcurrentRollbackConsistency(t *testing.T) {
	s, _ := New(Options{MaxRecords: 4, MaxNameBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			k := string(rune('a' + i))
			for j := 0; j < 30; j++ {
				// Second op deletes a missing key: whole batch must roll back.
				_, _ = s.Apply(Batch{Ops: []Op{{Put, k, []byte("v")}, {Delete, "zz", nil}}})
			}
		}()
	}
	w.Wait()
	z := s.Stats()
	if z.Records != 0 || z.Generation != 0 || z.NextRevision != 1 {
		t.Fatalf("rollback leaked state: %+v", z)
	}
}
