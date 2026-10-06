package topologygraph298

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("%+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{}); err != nil {
		t.Fatal(err)
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBatchNoSideEffects(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatal("ValidateBatch mutated state")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if err != ErrExists {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 1 {
		t.Fatal("no rollback")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	got := g.Snapshot()
	if got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatal("capacity failure must roll back")
	}
}

func TestEdgeCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 1, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 0 || g.Stats().Edges != 0 {
		t.Fatal(g.Stats())
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
		{DeleteNode, "b", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatal(s.Edges)
	}
}

func TestNotFoundAndExists(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "x", ""}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "x", "y"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "BAD"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatal(s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i, e := range wantE {
		if s.Edges[i] != e {
			t.Fatal(s.Edges)
		}
	}
	s.Nodes[0] = "mutated"
	s.Edges[0] = Edge{"x", "y"}
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependentAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("generation not preserved")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Edges != 1 || c.Stats().Edges != 0 {
		t.Fatal("clone shares state")
	}
	if ok, _ := g.Reachable("a", "b"); !ok {
		t.Fatal("original mutated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "n0"}}})
			_, _ = g.Reachable("n0", n)
			_ = g.Snapshot()
			_ = g.Stats()
			_, _ = g.Clone()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}})
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 16 {
		t.Fatal(got)
	}
}

func TestConcurrentApplySameNode(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	var w sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	w.Wait()
	if wins != 1 || g.Stats().Generation != 1 {
		t.Fatal(wins, g.Stats())
	}
}
