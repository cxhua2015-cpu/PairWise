package topologygraph403

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndNames(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
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
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); err != ErrInvalidInput {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok_nm-1", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchAndGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Stats().Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if r.Generation != 1 {
		t.Fatalf("generation: %d", r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	if g.Stats().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("rollback failed, nodes=%d", n)
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "c"}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("edge rollback failed")
	}
}

func TestDeleteNodeCascadeAndNotFound(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("cascade failed")
	}
	for _, b := range []Batch{
		{Ops: []Op{{DeleteNode, "missing", ""}}},
		{Ops: []Op{{AddEdge, "a", "missing"}}},
		{Ops: []Op{{DeleteEdge, "a", "c"}}},
	} {
		if _, err := g.Apply(b); err != ErrNotFound {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	}
	if _, err := g.Reachable("a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
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
		t.Fatal("clone diverges")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Reachable("a", "b"); !ok {
		t.Fatal("clone mutation leaked into original")
	}
	if c.Stats().Generation != g.Stats().Generation+1 {
		t.Fatal("clone clock not independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 512, MaxEdges: 512, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 50; j++ {
				m := fmt.Sprintf("%s-%d", n, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, m, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_ = g.Stats()
				c, err := g.Clone()
				if err == nil {
					_ = c.Snapshot()
				}
			}
		}()
	}
	wg.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 400 {
		t.Fatalf("nodes=%d", len(s.Nodes))
	}
	for i := 1; i < len(s.Nodes); i++ {
		if s.Nodes[i-1] >= s.Nodes[i] {
			t.Fatal("nodes not sorted")
		}
	}
	for i := 1; i < len(s.Edges); i++ {
		a, b := s.Edges[i-1], s.Edges[i]
		if a.From > b.From || (a.From == b.From && a.To >= b.To) {
			t.Fatal("edges not sorted")
		}
	}
}
