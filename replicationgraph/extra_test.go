package replicationgraph

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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_9", "abcdefgh"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	g := graph(t)
	// Unknown kind and extra field must fail even if earlier ops are valid.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "b", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Fails on the last op: duplicate edge.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state not rolled back")
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	// Temporarily exceeds MaxNodes mid-batch but ends within limits.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}, {DeleteNode, "d", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	// Ends over capacity: full rollback.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 3 {
		t.Fatal("state mutated after capacity failure")
	}
	// Edge capacity.
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "a", "c"}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("edges mutated after capacity failure")
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r0, e := g.Apply(Batch{})
	if e != nil || r0.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r1, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if r1.Generation != 1 {
		t.Fatal(r1)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
	r2, _ := g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if r2.Generation != 2 {
		t.Fatal(r2)
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
		{DeleteNode, "b", ""},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 {
		t.Fatal(s.Edges)
	}
	// Re-add b: previously blocking cycle path is gone.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "c", "a"}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestNotFoundAndExists(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "x", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "x", "y"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "x", "y"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("x", "y"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("bad name", "y"); !errors.Is(e, ErrInvalidInput) {
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

func TestSnapshotSortedAndDetached(t *testing.T) {
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
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot shares state with graph")
	}
}

func TestReachableTransitive(t *testing.T) {
	g, _ := New(Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "c", "d"},
	}})
	for _, tc := range []struct {
		from, to string
		want     bool
	}{{"a", "d", true}, {"d", "a", false}, {"a", "a", true}, {"b", "d", true}} {
		ok, e := g.Reachable(tc.from, tc.to)
		if e != nil || ok != tc.want {
			t.Fatalf("%s->%s: %v %v", tc.from, tc.to, ok, e)
		}
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
			m := fmt.Sprintf("n%02d", (i+1)%32)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
			_, _ = g.Reachable(n, m)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 {
		t.Fatal(len(s.Nodes))
	}
}
