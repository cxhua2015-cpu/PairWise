package topologygraph333

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{Kind: AddNode}}},
		{Ops: []Op{{Kind: AddNode, From: "A"}}},
		{Ops: []Op{{Kind: AddNode, From: "a b"}}},
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}}, // extra field
		{Ops: []Op{{Kind: DeleteNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a", To: "UPPER"}}},
		{Ops: []Op{{Kind: AddNode, From: "toolongname"}}}, // > MaxNameBytes
	}
	for _, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	// Structural validation happens before state reads: unknown kind must
	// win over ErrExists for an already-present node.
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidNames(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a-b_c0"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(e, r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}}})
	before := g.Snapshot()
	// Duplicate node mid-batch must roll back earlier ops.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Capacity overflow at batch end must roll back everything.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "d"}, {Kind: AddNode, From: "e"}, {Kind: AddNode, From: "f"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Cycle must roll back the node added earlier in the same batch.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "c"}, {Kind: AddEdge, From: "b", To: "c"}, {Kind: AddEdge, From: "c", To: "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batches")
	}
}

func TestSelfLoopAndDuplicateEdge(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "b", To: "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadeAndEdgeCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 2, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"},
		{Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "b", To: "c"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	// Deleting b frees an edge slot, so a->c fits within MaxEdges.
	if _, e = g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "b"}, {Kind: AddEdge, From: "a", To: "c"}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{From: "a", To: "c"}) {
		t.Fatal(s)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "c", To: "a"}, {Kind: AddEdge, From: "a", To: "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}, {"c", "a"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot shares state with graph")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if ok, e := g.Reachable("a", "a"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := g.Reachable("a", "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("bad name", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
			m := fmt.Sprintf("n%02d", (i+1)%16)
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: n}}})
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: n, To: m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: n, To: m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 16 {
		t.Fatal(len(s.Nodes))
	}
	// The graph must remain acyclic after concurrent edge churn.
	for _, e := range s.Edges {
		_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: e.From, To: e.To}}})
		if ok, _ := g.Reachable(e.To, e.From); ok {
			t.Fatalf("cycle via %v", e)
		}
		_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: e.From, To: e.To}}})
	}
}
