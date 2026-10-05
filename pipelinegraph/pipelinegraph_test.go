package pipelinegraph

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
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},                 // unknown kind
		{Ops: []Op{{Kind: 99, From: "a"}}},                // unknown kind
		{Ops: []Op{{Kind: AddNode, From: ""}}},            // empty name
		{Ops: []Op{{Kind: AddNode, From: "abcde"}}},       // too long
		{Ops: []Op{{Kind: AddNode, From: "A"}}},           // uppercase
		{Ops: []Op{{Kind: AddNode, From: "a b"}}},         // space
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}},  // extra field
		{Ops: []Op{{Kind: DeleteNode, To: "x"}}},          // missing name
		{Ops: []Op{{Kind: AddEdge, From: "a"}}},           // missing to
		{Ops: []Op{{Kind: DeleteEdge, To: "b"}}},          // missing from
		{Ops: []Op{{Kind: AddEdge, From: "a", To: "é"}}},  // non-ascii
	}
	for i, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: want ErrInvalidInput, got %v", i, err)
		}
	}
	if got := g.Snapshot().Generation; got != 0 {
		t.Fatalf("failed batches must not change generation, got %d", got)
	}
}

func TestValidNamesAccepted(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a-z_0"},
		{Kind: AddNode, From: "9"},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	must := func(b Batch) {
		t.Helper()
		if _, err := g.Apply(b); err != nil {
			t.Fatal(err)
		}
	}
	must(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	must(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "b", To: "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAtomicRollback(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Capacity is checked only at batch end: this batch would exceed MaxNodes.
	_, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("failed batch must roll back completely")
	}
	// Error mid-batch also rolls back earlier ops.
	_, err = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "b", To: "zz"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("failed batch must roll back completely")
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"},
		{Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "b", To: "c"},
		{Kind: DeleteNode, From: "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("got %+v", s)
	}
	if ok, _ := g.Reachable("a", "c"); ok {
		t.Fatal("a must not reach c after b deleted")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal("empty batch must not bump generation")
	}
	r, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}}})
	if r.Generation != 2 {
		t.Fatal("non-empty batch bumps generation exactly once")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "b", To: "c"}, {Kind: AddEdge, From: "a", To: "c"}, {Kind: AddEdge, From: "a", To: "b"},
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
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot slices must be isolated from internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("A", "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal("node must be reachable from itself")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n%02d", i)
			m := fmt.Sprintf("n%02d", (i+1)%32)
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: n}}})
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: n, To: m}}})
			_, _ = g.Reachable(n, m)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: n, To: m}}})
		}()
	}
	wg.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 {
		t.Fatal(len(s.Nodes))
	}
	if s.Generation == 0 {
		t.Fatal("generation must advance")
	}
}
