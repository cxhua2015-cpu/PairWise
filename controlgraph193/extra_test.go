package controlgraph193

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_", "12345678"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
}

func TestStructuralBeforeState(t *testing.T) {
	g := graph(t)
	// First op would succeed, second is structurally invalid: nothing applied.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "b", ""}}})
	if !errors.Is(e, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
	// Extra field on node op.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	b := g.Snapshot()
	// Duplicate node mid-batch must roll back the earlier adds.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) || g.Snapshot().Generation != b.Generation || len(g.Snapshot().Nodes) != 1 {
		t.Fatal(e)
	}
	// Missing edge endpoint.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// Delete missing node/edge.
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	// Second edge exceeds MaxEdges=1 and must roll back the node add too.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "c"}}})
	if !errors.Is(e, ErrCapacity) || len(g.Snapshot().Nodes) != 2 || len(g.Snapshot().Edges) != 1 {
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
	// Deleting the edge unblocks the previously cyclic edge.
	_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "c"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadeAndReachable(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "c"}}})
	_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal()
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "c"}, {AddEdge, "b", "c"}}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i := range wantN {
		if s.Nodes[i] != wantN[i] {
			t.Fatal(s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i := range wantE {
		if s.Edges[i] != wantE[i] {
			t.Fatal(s.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal(s2)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 400, MaxEdges: 800, MaxNameBytes: 12})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				n := fmt.Sprintf("n%d-%d", i, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}

func TestConcurrentEdgesAcyclic(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	var ops []Op
	for i := 0; i < 8; i++ {
		ops = append(ops, Op{AddNode, fmt.Sprintf("n%d", i), ""})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	// Race opposite-direction edges; at most one direction may succeed and
	// the graph must stay acyclic.
	var w sync.WaitGroup
	for k := 0; k < 8; k++ {
		k := k
		w.Add(2)
		go func() {
			defer w.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, fmt.Sprintf("n%d", k), fmt.Sprintf("n%d", (k+1)%8)}}})
		}()
		go func() {
			defer w.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, fmt.Sprintf("n%d", (k+1)%8), fmt.Sprintf("n%d", k)}}})
		}()
	}
	w.Wait()
	s := g.Snapshot()
	seen := map[Edge]bool{}
	for _, e := range s.Edges {
		if seen[Edge{e.To, e.From}] {
			t.Fatalf("both directions present: %v", e)
		}
		seen[e] = true
	}
}
