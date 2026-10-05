package configstack

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestNewInvalidOptions(t *testing.T) {
	valid := Options{MaxLayers: 1, MaxEntries: 1, MaxNameBytes: 1, MaxKeyBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
	bad := valid
	bad.MaxLayers = 0
	if _, e := New(bad); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	bad = valid
	bad.MaxTotalValueBytes = -1
	if _, e := New(bad); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	s := st(t)
	b := s.Snapshot()
	cases := []Op{
		{Kind: 0, Name: "a"},
		{Kind: 99, Name: "a"},
		{Kind: AddLayer, Name: "a", Key: "x"},
		{Kind: AddLayer, Name: "a", Position: 4},
		{Kind: AddLayer, Name: ""},
		{Kind: RemoveLayer, Name: "a", Position: 1},
		{Kind: Set, Name: "a", Key: "x", Value: nil},
		{Kind: Set, Name: "a", Key: "x", Value: []byte("012345678")}, // > MaxValueBytes
		{Kind: Delete, Name: "a", Key: "x", Value: []byte("v")},
		{Kind: Move, Name: "a", Position: -1},
		{Kind: Move, Name: "a", Key: "x"},
		{Kind: Set, Name: "a", Key: "bad key", Value: []byte("v")},
	}
	for _, op := range cases {
		if _, e := s.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed")
	}
}

func TestAddLayerPositionBounds(t *testing.T) {
	s := st(t)
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a", Position: 1}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a", Position: 0}, {Kind: AddLayer, Name: "b", Position: 1}}}); e != nil {
		t.Fatal(e)
	}
	if got := s.Snapshot().Layers[1].Name; got != "b" {
		t.Fatal(got)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	s := st(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}}})
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: RemoveLayer, Name: "z"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "z", Key: "x", Value: []byte("v")}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: Move, Name: "z", Position: 0}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestMoveToBottom(t *testing.T) {
	s := st(t)
	_, e := s.Apply(Batch{Ops: []Op{
		{Kind: AddLayer, Name: "a", Position: 0}, {Kind: AddLayer, Name: "b", Position: 1}, {Kind: AddLayer, Name: "c", Position: 2},
		{Kind: Move, Name: "a", Position: 2},
	}})
	if e != nil {
		t.Fatal(e)
	}
	var names []string
	for _, l := range s.Snapshot().Layers {
		names = append(names, l.Name)
	}
	if !reflect.DeepEqual(names, []string{"b", "c", "a"}) {
		t.Fatal(names)
	}
}

func TestCapacityRollback(t *testing.T) {
	s := st(t) // MaxTotalValueBytes 24
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Set, Name: "a", Key: "x", Value: []byte("12345678")}}})
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "a", Key: "y", Value: []byte("12345678")},
		{Kind: Set, Name: "a", Key: "z", Value: []byte("12345678")},
		{Kind: Set, Name: "a", Key: "w", Value: []byte("12345678")}, // 32 > 24 total
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
	// Entry-count capacity.
	_, e = s.Apply(Batch{Ops: []Op{
		{Kind: Set, Name: "a", Key: "k1", Value: []byte("v")},
		{Kind: Set, Name: "a", Key: "k2", Value: []byte("v")},
		{Kind: Set, Name: "a", Key: "k3", Value: []byte("v")},
		{Kind: Set, Name: "a", Key: "k4", Value: []byte("v")},
		{Kind: Set, Name: "a", Key: "k5", Value: []byte("v")},
		{Kind: Set, Name: "a", Key: "k6", Value: []byte("v")}, // 7 > MaxEntries 6
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Layer-count capacity.
	_, e = s.Apply(Batch{Ops: []Op{
		{Kind: AddLayer, Name: "l1"}, {Kind: AddLayer, Name: "l2"},
		{Kind: AddLayer, Name: "l3"}, {Kind: AddLayer, Name: "l4"}, // 5 > MaxLayers 4
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestChangedSurvivingOnly(t *testing.T) {
	s := st(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Kind: AddLayer, Name: "a"},
		{Kind: Set, Name: "a", Key: "gone", Value: []byte("v")},
		{Kind: Delete, Name: "a", Key: "gone"},
		{Kind: Set, Name: "a", Key: "kept", Value: []byte("v")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Changed) != 1 || r.Changed[0].Key != "kept" {
		t.Fatalf("%+v", r.Changed)
	}
	// Set on a layer removed later in the batch must not appear.
	r, e = s.Apply(Batch{Ops: []Op{
		{Kind: AddLayer, Name: "tmp"},
		{Kind: Set, Name: "tmp", Key: "k", Value: []byte("v")},
		{Kind: RemoveLayer, Name: "tmp"},
	}})
	if e != nil || len(r.Changed) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestChangedSortedByPriorityThenKey(t *testing.T) {
	s := st(t)
	r, e := s.Apply(Batch{Ops: []Op{
		{Kind: AddLayer, Name: "low"}, {Kind: AddLayer, Name: "high", Position: 0},
		{Kind: Set, Name: "low", Key: "b", Value: []byte("v")},
		{Kind: Set, Name: "high", Key: "z", Value: []byte("v")},
		{Kind: Set, Name: "low", Key: "a", Value: []byte("v")},
		{Kind: Set, Name: "high", Key: "a", Value: []byte("v")},
	}})
	if e != nil {
		t.Fatal(e)
	}
	var got []string
	for _, c := range r.Changed {
		got = append(got, c.Layer+"/"+c.Key)
	}
	want := []string{"high/a", "high/z", "low/a", "low/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v != %v", got, want)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	s := st(t)
	r, e := s.Apply(Batch{})
	if e != nil || r.Generation != 0 || r.Revision != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	r, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	// Failed batch must not consume generation or revision.
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Key: "x", Value: []byte("v")}, {Kind: RemoveLayer, Name: "zz"}}})
	r, _ = s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Key: "x", Value: []byte("v")}}})
	if r.Generation != 2 || r.Revision != 1 {
		t.Fatalf("%+v", r)
	}
	if s.Snapshot().NextRevision != 2 {
		t.Fatal(s.Snapshot().NextRevision)
	}
}

func TestResolveValidationAndMiss(t *testing.T) {
	s := st(t)
	if _, _, e := s.Resolve("bad key"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, f, e := s.Resolve("nope"); e != nil || f {
		t.Fatal(f, e)
	}
}

func TestSnapshotDeepCopy(t *testing.T) {
	s := st(t)
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Set, Name: "a", Key: "x", Value: []byte("abc")}}})
	snap := s.Snapshot()
	snap.Layers[0].Entries[0].Value[0] = 'z'
	e, _, _ := s.Resolve("x")
	if string(e.Value) != "abc" {
		t.Fatal(string(e.Value))
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxLayers: 64, MaxEntries: 64, MaxNameBytes: 8, MaxKeyBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 256})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("l%d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{
					{Kind: AddLayer, Name: n},
					{Kind: Set, Name: n, Key: "k", Value: []byte("v")},
					{Kind: Move, Name: n, Position: 0},
				}})
				_, _, _ = s.Resolve("k")
				_ = s.Snapshot()
				_, _ = s.Apply(Batch{Ops: []Op{{Kind: Delete, Name: n, Key: "k"}}})
			}
		}()
	}
	wg.Wait()
}
