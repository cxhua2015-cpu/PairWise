package topologygraph283

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsValidation(t *testing.T) {
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
		{Ops: []Op{{AddEdge, "a", ""}}},
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

func TestStateErrorsAndRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("rollback left %d nodes", n)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("rollback violated: %+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if z := g.Stats(); z.Nodes != 2 || z.Edges != 1 || z.Generation != 1 {
		t.Fatalf("committed state: %+v", z)
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if g.Stats().Generation != 1 {
		t.Fatal("failed batch bumped generation")
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
	if z := g.Stats(); z.Nodes != 0 || z.Edges != 0 || z.Generation != 0 {
		t.Fatalf("capacity rollback: %+v", z)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("bad name", "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestCloneIndependence(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone diverged")
	}
	_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if z := c.Stats(); z.Nodes != 2 || z.Edges != 1 || z.Generation != 1 {
		t.Fatalf("clone mutated by original: %+v", z)
	}
	if ok, _ := c.Reachable("a", "b"); !ok {
		t.Fatal("clone lost edge")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n-%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "hub"}}})
			_ = g.Snapshot()
			_ = g.Stats()
			_, _ = g.Reachable(n, n)
			_, _ = g.Clone()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}})
		}()
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}})
	w.Wait()
	z := g.Stats()
	if z.Nodes < 1 || int(z.Generation) < 1 {
		t.Fatalf("stats: %+v", z)
	}
	s := g.Snapshot()
	if s.Generation != z.Generation || len(s.Nodes) != z.Nodes || len(s.Edges) != z.Edges {
		t.Fatal("snapshot inconsistent with stats")
	}
}
