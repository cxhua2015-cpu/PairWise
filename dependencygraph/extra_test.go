package dependencygraph

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

func TestInvalidInput(t *testing.T) {
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
			t.Fatalf("case %d: %v", i, e)
		}
	}
	// Structural validation happens before state reads: unknown kind must
	// fail even if an earlier op would fail against state.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok", ""}, {Kind: 42, From: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("invalid batch must not mutate state")
	}
}

func TestValidNameCharset(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a-b_c1", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExistsNotFoundAndExists(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	mk(t, g, "a", "b")
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func mk(t *testing.T, g *Graph, names ...string) {
	t.Helper()
	var ops []Op
	for _, n := range names {
		ops = append(ops, Op{AddNode, n, ""})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	mk(t, g, "a")
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestBatchRollback(t *testing.T) {
	g := graph(t)
	mk(t, g, "a", "b")
	before := g.Snapshot()
	// Second op fails: first op must be rolled back.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "c", "ghost"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 2 {
		t.Fatalf("state changed: %+v", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, e := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	mk(t, g, "a", "b")
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("capacity failure must roll back")
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	// Edge capacity: delete + two adds within one batch exceeds MaxEdges=1.
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddNode, "c", ""}, {AddEdge, "b", "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	if g.Snapshot().Generation != 0 {
		t.Fatal()
	}
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
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
	mk(t, g, "a", "b", "c")
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("%+v", s.Edges)
	}
	if ok, e := g.Reachable("b", "a"); e == nil || ok {
		t.Fatal("deleted node must be gone")
	}
}

func TestReachable(t *testing.T) {
	g := graph(t)
	mk(t, g, "a", "b")
	if ok, e := g.Reachable("a", "a"); e != nil || !ok {
		t.Fatal("reflexive reachability")
	}
	if ok, e := g.Reachable("a", "b"); e != nil || ok {
		t.Fatal()
	}
	if _, e := g.Reachable("a", "ghost"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("ghost", "a"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	mk(t, g, "c", "a", "b")
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
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
	// Mutating the returned snapshot must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	mk(t, g, "root")
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "root", n}}})
				_, _ = g.Reachable("root", n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "root", n}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 1 || len(s.Edges) != 0 {
		t.Fatalf("%+v", s)
	}
}
