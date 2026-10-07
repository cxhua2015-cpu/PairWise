package topologygraph373

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustNew(t *testing.T, o Options) *Graph {
	t.Helper()
	g, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -2, 1}, {1, 1, -3},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, err)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "abcde", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{DeleteNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
		{Ops: []Op{{AddEdge, "a!", "b"}}},
	}
	for i, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: got %v", i, err)
		}
	}
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatal("invalid batches must not change state")
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	// Second op is structurally invalid; even though the first op would fail
	// with ErrNotFound against state, structural validation wins.
	_, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "missing", ""}, {Kind: 42}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestBatchAtomicity(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Fails midway (duplicate node); nothing may be applied.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatal("failed batch leaked partial state")
	}
	// Capacity exceeded only at the end: rollback.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("capacity failure leaked nodes")
	}
}

func TestSelfLoopAndCycleError(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 4, MaxEdges: 8, MaxNameBytes: 4})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
}

func TestEdgeErrors(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
	r, err = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {DeleteNode, "b", ""}}})
	if err != nil || r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r, err)
	}
}

func TestReachableErrors(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("A", "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
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
	// Mutating returned slices must not affect the graph.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"z", "z"}
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot shares memory with graph")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	w.Wait()
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatal(s)
	}
}

func TestConcurrentChainBuilders(t *testing.T) {
	g := mustNew(t, Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	var w sync.WaitGroup
	for wkr := 0; wkr < 8; wkr++ {
		wkr := wkr
		w.Add(1)
		go func() {
			defer w.Done()
			base := fmt.Sprintf("w%d", wkr)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, base, ""}}})
			for j := 0; j < 10; j++ {
				next := fmt.Sprintf("%s-%d", base, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, next, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, base, next}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 64 || len(s.Edges) > 64 {
		t.Fatal("capacity violated under concurrency")
	}
}
