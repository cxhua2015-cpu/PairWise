package topologygraph438

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for _, b := range bad {
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("validate %+v: %v", b, err)
		}
		if _, err := g.Apply(b); err != ErrInvalidInput {
			t.Fatalf("apply %+v: %v", b, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats() != (Stats{}) {
		t.Fatal("failed validation mutated state")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, err = g.Apply(Batch{Ops: []Op{}})
	if err != nil || r.Generation != 1 {
		t.Fatalf("empty batch after commit: %+v %v", r, err)
	}
}

func TestStateErrorsAndRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		batch Batch
		want  error
	}{
		{Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists},
		{Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound},
		{Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound},
		{Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound},
	}
	before := g.Snapshot()
	for _, c := range cases {
		if _, err := g.Apply(c.batch); !errors.Is(err, c.want) {
			t.Fatalf("%+v: want %v", c.batch, c.want)
		}
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("failed batches mutated state")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "c"}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
}

func TestReachableNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestCloneClockAndIsolation(t *testing.T) {
	g := graph(t)
	r, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() || c.Snapshot().Generation != r.Generation {
		t.Fatal("clone lost logical clock")
	}
	s := c.Snapshot()
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if g.Snapshot().Nodes[0] == "mutated" {
		t.Fatal("snapshot aliases internal state")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || c.Stats().Nodes != 1 {
		t.Fatal("clone shares state with original")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	batches := []Batch{
		{Ops: []Op{{AddEdge, "b", "a"}}},
		{Ops: []Op{{Kind: 42}}},
		{Ops: []Op{{AddNode, "a", ""}}},
		{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}},
	}
	for _, b := range batches {
		_, aerr := g.Apply(b)
		_, _, _, perr := g.Preview(b)
		if aerr != perr {
			t.Fatalf("%+v: apply=%v preview=%v", b, aerr, perr)
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := string(rune('a' + i))
			for j := 0; j < 10; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "zz"}}})
				_, _, _, _ = g.Preview(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_ = g.Stats()
				_, _ = g.Clone()
			}
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 16 {
		t.Fatal(got)
	}
	if g.Stats().Generation != 16 {
		t.Fatal(g.Stats())
	}
}
