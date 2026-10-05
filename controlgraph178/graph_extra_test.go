package controlgraph178

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
			t.Fatalf("options %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	bad := []Batch{
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
	for _, b := range bad {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, e)
		}
	}
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatalf("invalid batch mutated state: %+v", s)
	}
	if _, e := g.Reachable("BAD", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; even though the first op would
	// fail against state (duplicate node), structural check wins.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 42}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "ghost"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("cycle batch must roll back")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity failure must roll back, got %d nodes", n)
	}
	// Edge capacity exceeded only at batch end.
	g2, _ := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	_, e = g2.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g2.Snapshot().Edges) != 0 {
		t.Fatal("edge capacity failure must roll back")
	}
	// Mid-batch overflow that settles within capacity is fine.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if e != nil || len(g.Snapshot().Nodes) != 2 {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	// Failed batch does not bump generation.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}}})
	if r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r.Generation)
	}
}

func TestDeleteNodeCascadeAndReachable(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("cascade: %+v", s.Edges)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal("self reachability")
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "d", ""},
		{AddEdge, "c", "d"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c", "d"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "d"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating the returned snapshot must not affect the graph.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
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
	for _, e := range s.Edges {
		if _, ok := map[string]bool{}[e.From]; ok {
			t.Fatal(e)
		}
	}
}
