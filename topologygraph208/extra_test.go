package topologygraph208

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
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, e)
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
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: want ErrInvalidInput, got %v", i, e)
		}
	}
	// Structural failure anywhere aborts the whole batch before state reads.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok", ""}, {Kind: 7}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("invalid batch must not mutate state")
	}
}

func TestSemanticErrorsAndRollback(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("failed batch must roll back")
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	mustApply(t, g, []Op{{AddNode, "a", ""}, {AddNode, "b", ""}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "ghost"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	mustApply(t, g, []Op{{AddEdge, "a", "b"}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, e := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	mustApply(t, g, []Op{{AddNode, "a", ""}, {AddNode, "b", ""}})
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	mustApply(t, g, []Op{{AddEdge, "a", "b"}})
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if s.Generation != 2 || len(s.Nodes) != 2 || len(s.Edges) != 1 {
		t.Fatalf("state changed after failed batches: %+v", s)
	}
	// Temporary overflow within a batch is fine; only the end state counts.
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "b", "a"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, e)
	}
	mustApply(t, g, []Op{{AddNode, "a", ""}})
	r, e = g.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatalf("empty batch must not bump generation: %+v", r)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch must not bump generation")
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	mustApply(t, g, []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	})
	mustApply(t, g, []Op{{DeleteNode, "b", ""}})
	s := g.Snapshot()
	want := Snapshot{Generation: 2, Nodes: []string{"a", "c"}, Edges: []Edge{{From: "a", To: "c"}}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got %+v want %+v", s, want)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	mustApply(t, g, []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	wantEdges := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, wantEdges) {
		t.Fatal(s.Edges)
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot shares memory with internal state")
	}
}

func TestReachableSemantics(t *testing.T) {
	g := graph(t)
	mustApply(t, g, []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	})
	if ok, e := g.Reachable("a", "a"); e != nil || !ok {
		t.Fatal("node must be reachable from itself")
	}
	if ok, e := g.Reachable("c", "a"); e != nil || ok {
		t.Fatal("reverse direction must be unreachable")
	}
	if _, e := g.Reachable("a", "ghost"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("ghost", "a"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "BAD NAME"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, e := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			m := fmt.Sprintf("n%02d", (i+1)%32)
			for k := 0; k < 20; k++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				s := g.Snapshot()
				if len(s.Nodes) > 128 || len(s.Edges) > 256 {
					t.Errorf("capacity violated: %+v", s)
					return
				}
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 {
		t.Fatalf("want 32 nodes, got %d", len(s.Nodes))
	}
	if len(s.Edges) != 0 {
		t.Fatalf("want 0 edges, got %d", len(s.Edges))
	}
}

func mustApply(t *testing.T, g *Graph, ops []Op) Result {
	t.Helper()
	r, e := g.Apply(Batch{Ops: ops})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
