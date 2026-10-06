package topologygraph213

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
			t.Fatalf("opts=%+v err=%v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind(0), "a", ""}, {Kind(99), "a", ""},
		{AddNode, "", ""}, {AddNode, "A", ""}, {AddNode, "a b", ""},
		{AddNode, "a", "b"}, {DeleteNode, "a", "b"},
		{AddNode, "toolongname", ""},
		{AddEdge, "a", ""}, {AddEdge, "", "b"}, {DeleteEdge, "a", "B"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v err=%v", op, e)
		}
	}
	// Structural failure anywhere must abort before any state read:
	// a valid op followed by an invalid one must not commit anything.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "x", ""}}})
	if !errors.Is(e, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
}

func TestNameBoundaries(t *testing.T) {
	g := graph(t) // MaxNameBytes = 8
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "abcdefgh", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "abcdefghi", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a-z_0", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestRollbackAtomicity(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Fails mid-batch (duplicate node) -> nothing committed.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Fails at capacity check at batch end -> rollback.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Fails on missing edge delete -> rollback.
	_, e = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {DeleteEdge, "b", "a"}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	if g.Snapshot().Generation != 0 {
		t.Fatal()
	}
	r, e := g.Apply(Batch{}) // empty batch: no generation bump
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch must not bump generation.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) || g.Snapshot().Generation != 1 {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestEdgeErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
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
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot not isolated")
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
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}, {AddNode, n, ""}}})
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 32 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}

func TestConcurrentEdgesAcyclic(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 512, MaxNameBytes: 16})
	names := make([]string, 16)
	ops := make([]Op, 0, len(names)*2)
	for i := range names {
		names[i] = fmt.Sprintf("n%d", i)
		ops = append(ops, Op{AddNode, names[i], ""})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	// Concurrently try to add edges in both directions between pairs;
	// whatever succeeds, the graph must remain acyclic.
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		for j := i + 1; j < 16; j++ {
			i, j := i, j
			w.Add(2)
			go func() { defer w.Done(); _, _ = g.Apply(Batch{Ops: []Op{{AddEdge, names[i], names[j]}}}) }()
			go func() { defer w.Done(); _, _ = g.Apply(Batch{Ops: []Op{{AddEdge, names[j], names[i]}}}) }()
		}
	}
	w.Wait()
	s := g.Snapshot()
	// Kahn's algorithm on the snapshot.
	indeg := map[string]int{}
	adj := map[string][]string{}
	for _, e := range s.Edges {
		indeg[e.To]++
		adj[e.From] = append(adj[e.From], e.To)
	}
	var queue []string
	for _, n := range s.Nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	seen := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		seen++
		for _, nx := range adj[cur] {
			indeg[nx]--
			if indeg[nx] == 0 {
				queue = append(queue, nx)
			}
		}
	}
	if seen != len(s.Nodes) {
		t.Fatal("cycle detected after concurrent edge adds")
	}
}
