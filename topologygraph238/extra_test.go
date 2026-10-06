package topologygraph238

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
	cases := []Op{
		{Kind(0), "a", ""},
		{Kind(99), "a", ""},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "b"},
		{AddEdge, "a", ""},
		{AddEdge, "a", "a"},
		{DeleteEdge, "a", "A"},
	}
	for _, op := range cases {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestStatefulErrorsAndRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	gen := g.Snapshot().Generation
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	// Failed batch must roll back entirely: b must not survive.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 1 || s.Generation != gen {
		t.Fatalf("rollback: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); err != ErrCycle {
		t.Fatal(err)
	}
	g, _ = New(Options{MaxNodes: 3, MaxEdges: 2, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "c"}, {AddEdge, "b", "c"}, {AddEdge, "a", "b"}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if e := len(g.Snapshot().Edges); e != 1 {
		t.Fatal(e)
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
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatal(r)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err == nil {
		t.Fatal()
	}
	if g.Stats().Generation != 2 {
		t.Fatal(g.Stats())
	}
}

func TestReachableErrorsAndSelf(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatal(s.Nodes)
		}
	}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	for i, e := range wantEdges {
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
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 || c.Stats().Nodes != 1 {
		t.Fatal("clone aliases original")
	}
	if g.Stats().Generation != 1 || c.Stats().Generation != 2 {
		t.Fatal("generations not independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			for j := 0; j < 50; j++ {
				m := fmt.Sprintf("n%d-%d", i, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, m, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, m, n}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_ = g.Stats()
				_, _ = g.Clone()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, m, ""}}})
			}
		}()
	}
	w.Wait()
	s := g.Stats()
	if s.Nodes != 8 || s.Edges != 0 {
		t.Fatalf("final state: %+v", s)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
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
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}
