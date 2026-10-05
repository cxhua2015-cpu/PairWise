package controlgraph118

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
			t.Fatalf("opts=%+v err=%v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind(0), "a", ""},
		{Kind(9), "a", ""},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "a/b", ""},
		{AddNode, "toolongname", ""}, // > MaxNameBytes=8
		{AddNode, "a", "b"},          // extra field
		{DeleteNode, "a", "b"},
		{AddEdge, "a", ""},
		{AddEdge, "", "b"},
		{DeleteEdge, "a", "BAD"},
	}
	for _, op := range cases {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v err=%v", op, e)
		}
	}
	if _, e := g.Reachable("a", "!!"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; first op must not be applied
	// even though it would fail semantically anyway.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "x", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, e = g.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 {
		t.Fatalf("r=%+v e=%v", r, e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	mk := func(ops ...Op) error {
		_, e := g.Apply(Batch{Ops: ops})
		return e
	}
	if e := mk(Op{AddNode, "a", ""}, Op{AddNode, "a", ""}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteNode, "ghost", ""}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_ = mk(Op{AddNode, "a", ""}, Op{AddNode, "b", ""})
	if e := mk(Op{AddEdge, "a", "ghost"}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := mk(Op{AddEdge, "a", "b"}, Op{AddEdge, "a", "b"}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteEdge, "b", "a"}); !errors.Is(e, ErrNotFound) {
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

func TestFailedBatchRollsBack(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// Capacity exceeded only at the end: net edge count 7 > MaxEdges 6.
	ops := []Op{}
	for i := 0; i < 4; i++ {
		ops = append(ops, Op{AddNode, fmt.Sprintf("n%d", i), ""})
	}
	_, e := g.Apply(Batch{Ops: ops})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	if g.Snapshot().Generation != before.Generation {
		t.Fatal("generation changed after failed batch")
	}
}

func TestDeleteNodeCascadeAndCapacityAtEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	// Mid-batch the graph temporarily holds 3 nodes; only final counts matter.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteNode, "a", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 2 || len(s.Edges) != 0 {
		t.Fatalf("%+v", s)
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
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
	}
}

func TestReachableMissingNode(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
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
