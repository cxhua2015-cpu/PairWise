package topologygraph433

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Generation != 0 {
		t.Fatal("failed batches changed generation")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) || g.Stats().Generation != 0 {
		t.Fatal("capacity failure did not roll back")
	}
}

func TestEdgeCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 1, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 0 || g.Stats().Edges != 0 {
		t.Fatal("edge capacity failure did not roll back")
	}
}

func TestStateErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "x", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "x", "y"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "x", "y"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "a", "b"}, {AddEdge, "a", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}, {"a", "c"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].To = "mutated"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].To != "b" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("clone lost logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "z", ""}}}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().Nodes != 1 {
		t.Fatal("original mutation leaked into clone")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	batches := []Batch{
		{Ops: []Op{{AddEdge, "b", "a"}}},
		{Ops: []Op{{AddNode, "a", ""}}},
		{Ops: []Op{{DeleteNode, "nope", ""}}},
		{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}},
		{Ops: []Op{{Kind: 42, From: "a"}}},
	}
	for i, b := range batches {
		_, wantErr := g.Apply(b)
		_, _, _, gotErr := g.Preview(b)
		if gotErr != wantErr {
			t.Fatalf("case %d: preview=%v apply=%v", i, gotErr, wantErr)
		}
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("node-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _, _, _ = g.Preview(Batch{Ops: []Op{{AddNode, n + "-p", ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, "x", ""}}})
			_ = g.Stats()
			_ = g.Snapshot()
			_, _ = g.Reachable(n, n)
			if i%4 == 0 {
				_, _ = g.Clone()
			}
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
	if g.Stats().Generation != 32 {
		t.Fatal(g.Stats().Generation)
	}
}
