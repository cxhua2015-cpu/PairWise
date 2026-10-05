package controlgraph088

import (
	"errors"
	"fmt"
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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a.b", "é", "toolongname", "a/b"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok-name1", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	// Unknown kind.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(99), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Extra field: To set on AddNode.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Whole batch validated before state is read: second op invalid means
	// first op must not be applied.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(0), "", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("batch must be fully validated before applying")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	// Failed batch must not bump generation.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}}})
	if r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r)
	}
}

func TestEdgeErrors(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{AddEdge, "b", "a"}}}, ErrCycle)
	must(Batch{Ops: []Op{{AddEdge, "a", "a"}}}, ErrCycle)
	must(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("capacity failure must roll back the whole batch")
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Edge capacity exceeded at batch end.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatal(s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	for i, w := range wantE {
		if s.Edges[i] != w {
			t.Fatal(s.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("BAD!", "b"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Reachable(n, n)
			s := g.Snapshot()
			for j := 1; j < len(s.Nodes); j++ {
				if s.Nodes[j-1] >= s.Nodes[j] {
					t.Error("snapshot not sorted")
				}
			}
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 32 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}

func TestConcurrentEdgesAcyclic(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}})
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Edges) > 1 {
		t.Fatalf("cycle or duplicate edge slipped in: %v", s.Edges)
	}
}
