package topologygraph288

import (
	"errors"
	"fmt"
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
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range bad {
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
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if err != nil || r.Generation != 1 {
		t.Fatalf("%v %+v", err, r)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("%v %+v", err, r)
	}
	if g.Stats().Generation != 1 {
		t.Fatal(g.Stats())
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "x", ""}, {AddNode, "x", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if s := g.Snapshot(); s.Generation != before.Generation || len(s.Nodes) != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
	_, err = g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("capacity failure leaked")
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("cycle failure leaked edge")
	}
}

func TestDeleteNodeCascadeAndEdgeErrors(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "a", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Edges) != 0 || len(s.Nodes) != 1 {
		t.Fatalf("%+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "ghost"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "b"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("b", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "a"}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	want := []string{"a", "b", "c"}
	for i, n := range want {
		if s.Nodes[i] != n {
			t.Fatalf("order: %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[1] != (Edge{"c", "a"}) {
		t.Fatalf("edge order: %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("logical clock not preserved")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Reachable("a", "b"); !ok {
		t.Fatal("clone mutation leaked into original")
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "z", ""}}}); err != nil {
		t.Fatal(err)
	}
	if len(c.Snapshot().Nodes) != 1 {
		t.Fatal("original mutation leaked into clone")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n-%d", i)
			for j := 0; j < 20; j++ {
				m := fmt.Sprintf("n-%d-%d", i, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, m, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, m, n}}})
				_ = g.Stats()
				_ = g.Snapshot()
				_, _ = g.Reachable(n, m)
				_, _ = g.Clone()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, m, ""}}})
			}
		}()
	}
	w.Wait()
	s := g.Stats()
	if s.Nodes > 128 || s.Edges > 128 {
		t.Fatalf("capacity violated: %+v", s)
	}
	if snap := g.Snapshot(); len(snap.Nodes) != s.Nodes || len(snap.Edges) != s.Edges {
		t.Fatal("stats and snapshot diverged")
	}
}
