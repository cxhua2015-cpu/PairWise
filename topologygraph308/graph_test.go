package topologygraph308

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
			t.Fatalf("options %+v: got %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "中文", "toolongname", "a.b", "+x"}
	for _, n := range bad {
		_, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: n}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_9", "abcdefgh"} {
		if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: n}}}); e != nil {
			t.Fatalf("name %q: got %v", n, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{Kind: AddNode, From: "a", To: "b"},
		{Kind: DeleteNode, From: "a", To: "b"},
		{Kind: AddEdge, From: "a"},
		{Kind: DeleteEdge, To: "b"},
	}
	for _, op := range cases {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op %+v: got %v", op, e)
		}
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("invalid batches must not change generation")
	}
}

func TestValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; must fail even though first op is fine.
	_, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: 0, From: "b"}}})
	if !errors.Is(e, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); e != nil {
		t.Fatal(e)
	}
	r, e = g.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}}})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"}}})
	if !errors.Is(e, ErrCapacity) || len(g.Snapshot().Nodes) != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "b", To: "a"}}})
	if !errors.Is(e, ErrCycle) || len(g.Snapshot().Edges) != 0 {
		t.Fatal(e)
	}
}

func TestEdgeCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"},
		{Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "b", To: "c"},
	}})
	if !errors.Is(e, ErrCapacity) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "a", To: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"},
		{Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "b", To: "c"},
		{Kind: DeleteNode, From: "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatal(s)
	}
	// b's edges are gone, so c->a no longer cycles through b.
	if _, e = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "c", To: "a"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "c", To: "b"}, {Kind: AddEdge, From: "a", To: "c"}, {Kind: AddEdge, From: "a", To: "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot shares internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("zz", "a"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: n}}})
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "tmp"}, {Kind: DeleteNode, From: "tmp"}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: n}}})
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatal(s)
	}
}

func TestConcurrentEdgeBuilders(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	names := []string{"a", "b", "c", "d", "e", "f"}
	var ops []Op
	for _, n := range names {
		ops = append(ops, Op{Kind: AddNode, From: n})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < len(names); i++ {
		for j := 0; j < len(names); j++ {
			if i >= j {
				continue
			}
			from, to := names[i], names[j]
			w.Add(1)
			go func() {
				defer w.Done()
				// Forward edges always succeed; backward edges may hit ErrCycle.
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: from, To: to}}})
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: to, To: from}}})
			}()
		}
	}
	w.Wait()
	s := g.Snapshot()
	// Whatever the interleaving, the resulting graph must be acyclic.
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
		for _, next := range adj[cur] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if seen != len(s.Nodes) {
		t.Fatal("cycle detected in final graph")
	}
}
