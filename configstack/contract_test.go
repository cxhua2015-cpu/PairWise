package configstack

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func st(t *testing.T) *Stack {
	t.Helper()
	s, e := New(Options{MaxLayers: 4, MaxEntries: 6, MaxNameBytes: 12, MaxKeyBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 24})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestOrderResolveAndMove(t *testing.T) {
	s := st(t)
	_, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "base", Position: 0}, {Kind: AddLayer, Name: "top", Position: 0}, {Kind: Set, Name: "base", Key: "x", Value: []byte("b")}, {Kind: Set, Name: "top", Key: "x", Value: []byte("t")}}})
	if e != nil {
		t.Fatal(e)
	}
	x, f, _ := s.Resolve("x")
	if !f || string(x.Value) != "t" {
		t.Fatal(x)
	}
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: Move, Name: "top", Position: 1}}})
	x, _, _ = s.Resolve("x")
	if string(x.Value) != "b" {
		t.Fatal(x)
	}
}
func TestSequentialRevisionAndRollback(t *testing.T) {
	s := st(t)
	x, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Set, Name: "a", Key: "x", Value: []byte("1")}, {Kind: Set, Name: "a", Key: "x", Value: []byte("2")}}})
	if e != nil || x.Revision != 2 || len(x.Changed) != 1 || x.Changed[0].Revision != 2 {
		t.Fatalf("%+v %v", x, e)
	}
	b := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{{Kind: Set, Name: "a", Key: "y", Value: []byte("3")}, {Kind: Delete, Name: "a", Key: "missing"}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	s, _ := New(Options{MaxLayers: 1, MaxEntries: 1, MaxNameBytes: 8, MaxKeyBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Set, Name: "a", Key: "x", Value: []byte("1234")}}})
	_, e := s.Apply(Batch{Ops: []Op{{Kind: Delete, Name: "a", Key: "x"}, {Kind: Set, Name: "a", Key: "y", Value: []byte("zz")}}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestOwnershipAndSnapshot(t *testing.T) {
	s := st(t)
	v := []byte("abc")
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "a"}, {Kind: Set, Name: "a", Key: "x", Value: v}}})
	v[0] = 'z'
	x, _, _ := s.Resolve("x")
	x.Value[0] = 'y'
	y, _, _ := s.Resolve("x")
	if string(y.Value) != "abc" {
		t.Fatal(string(y.Value))
	}
}
func TestValidation(t *testing.T) {
	s := st(t)
	b := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: "bad?"}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	s, _ := New(Options{MaxLayers: 32, MaxEntries: 32, MaxNameBytes: 8, MaxKeyBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 64})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := string(rune('a' + i))
			_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddLayer, Name: n, Position: 0}, {Kind: Set, Name: n, Key: "x", Value: []byte("v")}}})
			_, _, _ = s.Resolve("x")
			_ = s.Snapshot()
		}()
	}
	wg.Wait()
	if len(s.Snapshot().Layers) != 20 {
		t.Fatal(len(s.Snapshot().Layers))
	}
}
