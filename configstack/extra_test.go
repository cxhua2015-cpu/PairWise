package configstack

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{},
		{MaxLayers: 1, MaxEntries: 1, MaxNameBytes: 1, MaxKeyBytes: 1, MaxValueBytes: 1},
		{MaxLayers: -1, MaxEntries: 1, MaxNameBytes: 1, MaxKeyBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestStructuralValidationRejectsWholeBatch(t *testing.T) {
	s := st(t)
	b := s.Snapshot()
	cases := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Kind: AddLayer, Name: "a", Key: "x"},
		{Kind: AddLayer, Name: "a", Position: 4},
		{Kind: AddLayer, Name: "a", Position: -1},
		{Kind: RemoveLayer, Name: "a", Position: 1},
		{Kind: Set, Name: "a", Key: "x", Value: []byte("123456789")},
		{Kind: Set, Name: "a", Key: "x"},
		{Kind: Set, Name: "a", Key: "x", Value: []byte("v"), Position: 1},
		{Kind: Delete, Name: "a", Key: "x", Value: []byte("v")},
		{Kind: Move, Name: "a", Position: -1},
		{Kind: Move, Name: "a", Key: "x"},
		{Kind: AddLayer, Name: ""},
		{Kind: AddLayer, Name: "has space"},
		{Kind: AddLayer, Name: "this-name-is-too-long"},
		{Kind: Set, Name: "a", Key: "bad$key", Value: []byte("v")},
	}
	for _, op := range cases {
		_, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "ok"}, op}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	if got := s.Snapshot(); got.Generation != b.Generation || len(got.Layers) != 0 {
		t.Fatalf("state mutated: %+v", got)
	}
}

func TestExecutionPositionBounds(t *testing.T) {
	s := st(t)
	// Position structurally valid (<= MaxLayers-1) but beyond candidate count.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a", Position: 2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Move, Name: "a", Position: 2}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Move to bottom (Position == resulting length) is allowed.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: AddLayer, Name: "b"}, {Kind: Move, Name: "a", Position: 1}}}); e != nil {
		t.Fatal(e)
	}
	if got := s.Snapshot().Layers; got[0].Name != "b" || got[1].Name != "a" {
		t.Fatal(got)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	s := st(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}}})
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	for _, op := range []Op{
		{Kind: RemoveLayer, Name: "z"},
		{Kind: Set, Name: "z", Key: "x", Value: []byte("v")},
		{Kind: Delete, Name: "a", Key: "x"},
		{Kind: Move, Name: "z"},
	} {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrNotFound) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
}

func TestCapacityRollbackAndCounters(t *testing.T) {
	s, _ := New(Options{MaxLayers: 2, MaxEntries: 2, MaxNameBytes: 8, MaxKeyBytes: 8, MaxValueBytes: 8, MaxTotalValueBytes: 8})
	r1, _ := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Set, Name: "a", Key: "x", Value: []byte("1234")}}})
	// Final byte capacity exceeded: everything rolls back, no generation/revision spent.
	_, e := s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Key: "y", Value: []byte("1234")}, {Kind: Set, Name: "a", Key: "z", Value: []byte("1234")}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	r2, e := s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Key: "y", Value: []byte("12")}}})
	if e != nil || r2.Generation != r1.Generation+1 || r2.Revision != r1.Revision+1 {
		t.Fatalf("%+v %v", r2, e)
	}
	// Final layer count exceeded.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "b"}, {Kind: AddLayer, Name: "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Final entry count exceeded.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Key: "p", Value: []byte("1")}, {Kind: Set, Name: "a", Key: "q", Value: []byte("1")}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestEmptyBatchAndChanged(t *testing.T) {
	s := st(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	r, _ = s.Apply(Batch{Ops: []Op{
		{Kind: AddLayer, Name: "lo", Position: 0},
		{Kind: AddLayer, Name: "hi", Position: 0},
		{Kind: Set, Name: "lo", Key: "b", Value: []byte("1")},
		{Kind: Set, Name: "hi", Key: "a", Value: []byte("2")},
		{Kind: Set, Name: "lo", Key: "c", Value: []byte("3")},
		{Kind: Delete, Name: "lo", Key: "c"},
	}})
	if r.Generation != 1 || len(r.Changed) != 2 || r.Changed[0].Key != "a" || r.Changed[1].Key != "b" {
		t.Fatalf("%+v", r)
	}
	// Set then RemoveLayer: entry does not survive, Changed empty.
	r, _ = s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "hi", Key: "z", Value: []byte("9")}, {Kind: RemoveLayer, Name: "hi"}}})
	if len(r.Changed) != 0 {
		t.Fatalf("%+v", r.Changed)
	}
}

func TestResolveValidationAndMiss(t *testing.T) {
	s := st(t)
	if _, _, e := s.Resolve("bad key"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, f, e := s.Resolve("missing"); f || e != nil {
		t.Fatal(f, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxLayers: 64, MaxEntries: 64, MaxNameBytes: 8, MaxKeyBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "base"}}})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i%4)
			val := []byte{byte('a' + i)}
			for j := 0; j < 50; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "base", Key: key, Value: val}}})
				if e, f, err := s.Resolve(key); err == nil && f && len(e.Value) != 1 {
					t.Errorf("bad value %q", e.Value)
				}
				snap := s.Snapshot()
				if len(snap.Layers) != 1 {
					t.Errorf("layers=%d", len(snap.Layers))
				}
				_, _ = s.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "base", Key: key}}})
			}
		}()
	}
	wg.Wait()
}
