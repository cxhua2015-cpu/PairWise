package topologygraph303

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -2, 1}, {1, 1, -3},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{Kind: AddNode, From: ""},
		{Kind: AddNode, From: "A"},
		{Kind: AddNode, From: "a b"},
		{Kind: AddNode, From: "a.b"},
		{Kind: AddNode, From: "toolongname"}, // > 8 bytes
		{Kind: AddNode, From: "a", To: "b"},   // extra field
		{Kind: DeleteNode, From: "a", To: "b"},
		{Kind: AddEdge, From: "a"},
		{Kind: AddEdge, From: "a", To: ""},
		{Kind: DeleteEdge, From: "", To: "b"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, e)
		}
	}
	// Structural validation happens before any state read: an invalid op
	// anywhere in the batch aborts it, even after valid ops.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "ok"}, {Kind: 7, From: "x"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("invalid batch mutated state")
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation bumped on failed batch")
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, e)
	}
	r, e = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if e != nil || r.Generation != 1 {
		t.Fatalf("first batch: %+v %v", r, e)
	}
	// Failed batch must not bump generation.
	if _, e = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, e = g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "a"}}})
	if e != nil || r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatalf("gen: %+v %v", r, e)
	}
}

func TestRollbackAtomicity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"},
		{Kind: AddNode, From: "b"},
		{Kind: AddNode, From: "c"}, // exceeds capacity only at batch end
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("state leaked from failed batch: %+v", s)
	}
	// Edge capacity exceeded at end after intermediate delete: allowed.
	_, e = g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"},
		{Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "a", To: "b"},
		{Kind: DeleteEdge, From: "a", To: "b"},
		{Kind: AddEdge, From: "b", To: "a"},
	}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	must := func(b Batch) {
		t.Helper()
		if _, e := g.Apply(b); e != nil {
			t.Fatal(e)
		}
	}
	must(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "zz", To: "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	must(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "b", To: "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Self-loop is a cycle.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "c", To: "a"}, {Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "c", To: "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s1 := g.Snapshot()
	if !reflect.DeepEqual(s1.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s1.Nodes)
	}
	want := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if !reflect.DeepEqual(s1.Edges, want) {
		t.Fatal(s1.Edges)
	}
	// Mutating returned slices must not affect internal state.
	s1.Nodes[0] = "zz"
	s1.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if !reflect.DeepEqual(s2.Nodes, []string{"a", "b", "c"}) || !reflect.DeepEqual(s2.Edges, want) {
		t.Fatal("snapshot shares memory with graph")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("bad name", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); e != nil {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 400, MaxEdges: 400, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				n := fmt.Sprintf("n%d-%d", i, j)
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: n}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: n}}})
			}
		}()
	}
	// Concurrent edge writers on a shared chain; cycle errors are fine.
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "x"}, {Kind: AddNode, From: "y"}}})
	for i := 0; i < 4; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 30; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "x", To: "y"}}})
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "y", To: "x"}}})
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "x", To: "y"}}})
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "y", To: "x"}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 2 {
		t.Fatalf("nodes: %v", s.Nodes)
	}
	if len(s.Edges) > 2 {
		t.Fatalf("edges: %v", s.Edges)
	}
}
