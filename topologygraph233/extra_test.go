package topologygraph233

import (
	"errors"
	"fmt"
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
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
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
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("generation moved on empty batch: %+v", r)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("rollback failed, nodes=%d", n)
	}
	if g.Stats().Generation != 0 {
		t.Fatal("generation moved on failed batch")
	}
}

func TestRollbackOnErrorMidBatch(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}})
	if err != ErrExists || len(g.Snapshot().Nodes) != 0 {
		t.Fatalf("%v %v", err, g.Snapshot())
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestDeleteNodeCascadeAndCloneIsolation(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("clone lost generation")
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("cascade failed")
	}
	if len(c.Snapshot().Edges) != 2 {
		t.Fatal("clone aliased original")
	}
}

func TestReachableNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
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
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
				_ = g.Stats()
				_ = g.Snapshot()
				_, _ = g.Reachable(n, n)
				if j%5 == 0 {
					_, _ = g.Clone()
				}
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	s := g.Stats()
	if s.Nodes != 0 || s.Edges != 0 {
		t.Fatalf("leaked state: %+v", s)
	}
}

func TestConcurrentAcyclicInvariant(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 256, MaxNameBytes: 8})
	nodes := []string{"a", "b", "c", "d", "e", "f"}
	var ops []Op
	for _, n := range nodes {
		ops = append(ops, Op{AddNode, n, ""})
	}
	if _, err := g.Apply(Batch{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	var w sync.WaitGroup
	for i := 0; i < len(nodes); i++ {
		for j := 0; j < len(nodes); j++ {
			if i == j {
				continue
			}
			from, to := nodes[i], nodes[j]
			w.Add(1)
			go func() {
				defer w.Done()
				_, err := g.Apply(Batch{Ops: []Op{{AddEdge, from, to}}})
				if err != nil && !errors.Is(err, ErrCycle) && !errors.Is(err, ErrExists) && !errors.Is(err, ErrCapacity) {
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
	}
	w.Wait()
	snap := g.Snapshot()
	// Kahn's algorithm on the snapshot: the graph must remain acyclic.
	indeg := map[string]int{}
	adj := map[string][]string{}
	for _, e := range snap.Edges {
		indeg[e.To]++
		adj[e.From] = append(adj[e.From], e.To)
	}
	var queue []string
	for _, n := range snap.Nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	seen := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		seen++
		for _, m := range adj[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if seen != len(snap.Nodes) {
		t.Fatal("cycle detected in committed state")
	}
}
