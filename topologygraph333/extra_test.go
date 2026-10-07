package topologygraph333

import (
	"errors"
	"fmt"
	"reflect"
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

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Unknown kind, even though the first op would also fail state checks.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "x", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Extra field To on AddNode.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Invalid names: empty, uppercase, space, too long.
	for _, n := range []string{"", "A", "a b", "abcdefghi", "a.b"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	// Valid charset: lowercase, digits, hyphen, underscore.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a0-_", ""}}}); e != nil {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 1 {
		t.Fatal("failed batch must not mutate state")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{}})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal(s)
	}
	// Edge capacity: mid-batch delete makes room, but net over capacity fails.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) && !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 1 {
		t.Fatal(g.Snapshot())
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadeAndReachable(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatal(s)
	}
	if ok, _ := g.Reachable("a", "c"); !ok {
		t.Fatal()
	}
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "BAD"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if ok, _ := g.Reachable("a", "a"); !ok {
		t.Fatal("self must be reachable")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "b"}, {AddEdge, "a", "c"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zzz"
	s.Edges[0].From = "zzz"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot must be isolated from internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			m := fmt.Sprintf("n%02d", (i+1)%16)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 128 || len(s.Edges) > 128 {
		t.Fatal("capacity violated")
	}
}
