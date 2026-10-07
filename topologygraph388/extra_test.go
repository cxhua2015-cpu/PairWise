package topologygraph388

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"}, {Kind: 99, From: "a"},
		{AddNode, "", ""}, {AddNode, "A", ""}, {AddNode, "a b", ""},
		{AddNode, "a/b", ""}, {AddNode, "toolongname", ""}, {AddNode, "a", "b"},
		{AddEdge, "a", ""}, {DeleteEdge, "", "b"}, {AddEdge, "a", "B"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if _, e := g.Reachable("a", "BAD"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("state mutated by invalid batches")
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}) // fails
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch changed generation")
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatal(r.Generation)
	}
}

func TestRollbackAtomic(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// Last op fails -> entire batch rolled back.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "a", "c"}, {DeleteNode, "zz", ""}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Capacity overflow at batch end -> rolled back.
	g2, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	_, _ = g2.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	_, e = g2.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}})
	if !errors.Is(e, ErrCapacity) || len(g2.Snapshot().Nodes) != 2 {
		t.Fatal(e)
	}
}

func TestSelfLoopAndTransitiveCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "c"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{From: "a", To: "c"}) {
		t.Fatal(s.Edges)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{From: "a", To: "b"}, {From: "a", To: "c"}, {From: "c", To: "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{From: "z", To: "z"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
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
			n := fmt.Sprintf("n%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			if i > 0 {
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, fmt.Sprintf("n%d", i-1), n}}})
			}
			_, _ = g.Reachable("n0", n)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}, {AddNode, n, ""}}})
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 32 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}
