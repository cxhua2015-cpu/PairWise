package topologygraph248

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "extra"},
		{DeleteNode, "a", "extra"},
		{AddEdge, "a", ""},
		{AddEdge, "a", "a"},
		{DeleteEdge, "", "b"},
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok_1-x", ""}}}); err != nil {
		t.Fatal(err)
	}
	if got := g.Stats(); got.Nodes != 0 || got.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%v %+v", err, r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// Fails at the end: duplicate node add.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "c"}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("not rolled back: %+v", got)
	}
	// Fails on missing edge delete.
	_, err = g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}, {DeleteEdge, "x", "y"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	// Mid-batch overflow is fine; only the final count is checked.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteNode, "a", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if n := g.Stats().Nodes; n != 2 {
		t.Fatal(n)
	}
}

func TestDeleteNodeCascadeAndReadd(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	_, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("%+v", s)
	}
	// Re-adding b must allow fresh edges (no stale index entries).
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCycleDetectionDeep(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	ops := []Op{}
	for _, n := range []string{"a", "b", "c", "d"} {
		ops = append(ops, Op{AddNode, n, ""})
	}
	ops = append(ops, Op{AddEdge, "a", "b"}, Op{AddEdge, "b", "c"}, Op{AddEdge, "c", "d"})
	if _, err := g.Apply(Batch{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "d", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "d", "b"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "d"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Reachable("a", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("BAD NAME", "a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
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
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("generation not preserved")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 {
		t.Fatal("clone mutated original")
	}
	if c.Stats().Nodes != 1 || c.Stats().Edges != 0 {
		t.Fatal("original mutated clone")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("node-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			_ = g.Stats()
			if i%4 == 0 {
				c, err := g.Clone()
				if err == nil {
					_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				}
			}
		}()
	}
	wg.Wait()
	if got := g.Stats().Nodes; got != 32 {
		t.Fatal(got)
	}
	if got := g.Stats().Generation; got != 32 {
		t.Fatal(got)
	}
}

func TestConcurrentEdgeWriters(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 16})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "src", ""}, {AddNode, "dst", ""}}})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "src", "dst"}}})
		}()
	}
	wg.Wait()
	if got := g.Stats().Edges; got != 1 {
		t.Fatal(got)
	}
}
