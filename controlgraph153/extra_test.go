package controlgraph153

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "a.b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "", "b"}}},
		{Ops: []Op{{DeleteEdge, "a", "B"}}},
		// one bad op invalidates the whole batch
		{Ops: []Op{{AddNode, "ok", ""}, {AddNode, "bad!", ""}}},
	}
	for _, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatalf("state mutated: %+v", s)
	}
	if _, e := g.Reachable("bad!", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}, ErrNotFound)
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("zz", "a"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("batch must roll back")
	}
}

func TestCycleRollback(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "c", "d"}, {AddEdge, "d", "a"},
	}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	// Final state exceeds node capacity -> whole batch rolls back.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("rollback failed")
	}
	// Intermediate overflow is fine if the final state fits.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	// Edge capacity exceeded at end.
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCapacity) && !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("rollback failed")
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if r.Generation != 2 {
		t.Fatal(r)
	}
	// Failed batches do not bump generation.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
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
	// Mutating the returned snapshot must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot not isolated")
	}
}

func TestReachableTransitive(t *testing.T) {
	g, _ := New(Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "c", "d"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		from, to string
		want     bool
	}{{"a", "d", true}, {"a", "a", true}, {"d", "a", false}, {"b", "d", true}, {"c", "a", false}} {
		ok, e := g.Reachable(tc.from, tc.to)
		if e != nil || ok != tc.want {
			t.Fatalf("%s->%s: %v %v", tc.from, tc.to, ok, e)
		}
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
			for j := 0; j < 30; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	w.Wait()
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatalf("leaked state: %+v", s)
	}
}

func TestConcurrentDAGInvariant(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 512, MaxNameBytes: 8})
	names := make([]string, 0, 26)
	for c := 'a'; c <= 'z'; c++ {
		names = append(names, string(c))
	}
	ops := []Op{}
	for _, n := range names {
		ops = append(ops, Op{AddNode, n, ""})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for a := 0; a < len(names); a++ {
				for b := 0; b < len(names); b++ {
					if (a+b)%8 != i || a == b {
						continue
					}
					_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, names[a], names[b]}}})
				}
			}
		}()
	}
	w.Wait()
	// Whatever edges won, the graph must still be acyclic: a topological
	// order must exist.
	s := g.Snapshot()
	indeg := map[string]int{}
	for _, n := range s.Nodes {
		indeg[n] = 0
	}
	for _, e := range s.Edges {
		indeg[e.To]++
	}
	queue := []string{}
	for n, d := range indeg {
		if d == 0 {
			queue = append(queue, n)
		}
	}
	adj := map[string][]string{}
	for _, e := range s.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		seen++
		for _, m := range adj[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if seen != len(s.Nodes) {
		t.Fatal("cycle detected after concurrent adds")
	}
}
