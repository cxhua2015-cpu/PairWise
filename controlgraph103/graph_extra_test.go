package controlgraph103

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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "a/b", ""}}},
		{Ops: []Op{{AddNode, "é", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}}, // > MaxNameBytes(8)
		{Ops: []Op{{AddNode, "a", "b"}}},           // extra To field
		{Ops: []Op{{DeleteNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: want ErrInvalidInput, got %v", i, err)
		}
	}
	// A structurally invalid op later in the batch must fail the whole batch
	// before any state check: "zz" would be ErrExists-adjacent but never runs.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 42}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("state mutated by invalid batch: %d nodes", n)
	}
}

func TestValidNamesAccepted(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a-0_", ""}, {AddNode, "zzzzzzzz", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{Ops: nil})
	if err != nil || r.Generation != 1 {
		t.Fatalf("nil batch after commit: %+v %v", r, err)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("generation changed on empty batch")
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, err := g.Apply(b); !errors.Is(err, want) {
			t.Fatalf("want %v, got %v", want, err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "b", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, nil)
}

func TestSelfLoopIsCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Cycle failure mid-batch must roll back earlier ops in the same batch.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// Capacity failure must also roll back.
	g2, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	if _, err := g2.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	_, err = g2.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g2.Snapshot().Nodes; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("rollback failed: %v", got)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	g, _ := New(Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	want := []Edge{{"a", "c"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatalf("edges after cascade: %v", s.Edges)
	}
	// Edge capacity freed by cascade allows new edges.
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "c"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	s2 := g.Snapshot()
	if s2.Nodes[0] == "mutated" || s2.Edges[0].From == "mutated" {
		t.Fatal("snapshot shares memory with graph")
	}
	if !reflect.DeepEqual(s2.Nodes, []string{"a", "b"}) {
		t.Fatalf("nodes not sorted: %v", s2.Nodes)
	}
}

func TestSnapshotEdgeOrdering(t *testing.T) {
	g, _ := New(Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "b", ""}, {AddNode, "a", ""}, {AddNode, "c", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	want := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatalf("edge order: %v", s.Edges)
	}
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatalf("node order: %v", s.Nodes)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("A", "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("self reachability: %v %v", ok, err)
	}
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
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
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 16 {
		t.Fatalf("nodes: %d", len(s.Nodes))
	}
	if len(s.Edges) != 0 {
		t.Fatalf("edges left: %v", s.Edges)
	}
}
