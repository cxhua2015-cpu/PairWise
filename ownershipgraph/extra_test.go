package ownershipgraph

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func mustGraph(t *testing.T, o Options) *Graph {
	t.Helper()
	g, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0},
		{-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "abcde", ""}}}, // too long
		{Ops: []Op{{AddNode, "a", "b"}}},    // extra field
		{Ops: []Op{{DeleteNode, "a", "b"}}}, // extra field
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for _, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: got %v", b, err)
		}
	}
	// Structural failure anywhere must abort before state is read:
	// a valid op followed by an invalid one must not apply.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("invalid batch mutated state")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if err != nil || r.Generation != 1 {
		t.Fatalf("first batch: %+v %v", r, err)
	}
	r, err = g.Apply(Batch{Ops: []Op{}})
	if err != nil || r.Generation != 1 {
		t.Fatalf("empty batch after: %+v %v", r, err)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("generation drifted")
	}
}

func TestRollbackOnError(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	before := g.Snapshot()
	// Duplicate add inside one batch must roll back the first add.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	// Missing node for edge.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "zz"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Self-loop is a cycle.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("failed batches mutated state")
	}
}

func TestCapacityOnlyAtEnd(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 4})
	// Exceed node capacity mid-batch but return under the limit.
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Final state over capacity fails and rolls back.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot().Nodes; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatal(got)
	}
	// Edge capacity checked at end too.
	_, err = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) { // b->a after a->b is a cycle
		t.Fatal(err)
	}
	g2 := mustGraph(t, Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 4})
	_, err = g2.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(g2.Snapshot().Edges) != 0 {
		t.Fatal("capacity failure leaked edges")
	}
}

func TestDeleteNodeCascadeAndReadd(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("cascade failed: %+v", s)
	}
	// Deleting again must fail.
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Deleting a removed edge must fail.
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestReachableErrorsAndSelf(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 4})
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("bad name", "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal("self should be reachable")
	}
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating the snapshot must not affect the graph.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"z", "z"}
	if got := g.Snapshot(); got.Nodes[0] != "a" || got.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot shares state with graph")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n-%d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	w.Wait()
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatalf("expected empty graph, got %+v", s)
	}
}

func TestConcurrentEdgesStayAcyclic(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 4, MaxEdges: 16, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Edges) > 1 {
		t.Fatalf("invariant broken: %+v", s.Edges)
	}
	if len(s.Edges) == 1 {
		ok, err := g.Reachable(s.Edges[0].To, s.Edges[0].From)
		if err != nil || ok {
			t.Fatal("graph contains a cycle")
		}
	}
}
