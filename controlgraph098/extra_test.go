package controlgraph098

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

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"}, {Kind: 99, From: "a"},
		{AddNode, "", ""}, {AddNode, "A", ""}, {AddNode, "a b", ""},
		{AddNode, "a.b", ""}, {AddNode, "工具", ""}, {AddNode, "toolongname", ""},
		{AddNode, "a", "b"}, // extra field
		{AddEdge, "a", ""}, {AddEdge, "", "b"}, {AddEdge, "A", "b"},
		{DeleteNode, "a", "x"}, {DeleteEdge, "a", "B"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v err=%v", op, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; must fail with ErrInvalidInput,
	// not with a state error, and nothing may be applied.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 42}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("batch leaked")
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
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
}

func TestSelfLoopIsCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// Capacity exceeded only at the end: whole batch rolls back.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// Edge capacity: 6 allowed, push past it.
	_, e = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""},
		{AddEdge, "a", "c"}, {AddEdge, "a", "d"}, {AddEdge, "b", "c"},
		{AddEdge, "b", "d"}, {AddEdge, "c", "d"}, {AddEdge, "c", "e"}, {AddEdge, "d", "e"},
	}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r0, e := g.Apply(Batch{})
	if e != nil || r0.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(r0, e)
	}
	r1, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r2, _ := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r1.Generation != 1 || r2.Generation != 2 {
		t.Fatal(r1, r2)
	}
	// Failed batch must not bump generation.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r3, _ := g.Apply(Batch{})
	if r3.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r3)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating returned slices must not affect the graph.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
	}
}

func TestReachableTransitiveAndMissing(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"a", "a", true}, {"a", "c", true}, {"c", "a", false},
		{"a", "d", false}, {"zz", "a", false}, {"a", "zz", false},
	} {
		ok, e := g.Reachable(tc.from, tc.to)
		if e != nil || ok != tc.want {
			t.Fatalf("%s->%s: %v %v", tc.from, tc.to, ok, e)
		}
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "c", "d"},
	}})
	_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"c", "d"}) {
		t.Fatal(s.Edges)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, fmt.Sprintf("n%02d", (i+1)%32)}}})
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}, {AddNode, n, ""}}})
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
}
