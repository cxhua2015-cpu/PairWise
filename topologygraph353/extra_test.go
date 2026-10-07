package topologygraph353

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
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -2, 1}, {1, 1, -3},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 4})
	bad := []Op{
		{Kind: AddNode, From: ""},           // empty
		{Kind: AddNode, From: "Ab"},         // uppercase
		{Kind: AddNode, From: "a b"},        // space
		{Kind: AddNode, From: "abcde"},      // too long
		{Kind: AddNode, From: "é"},          // non-ASCII
		{Kind: AddNode, From: "a", To: "b"}, // extra field for node op
		{Kind: AddEdge, From: "a"},          // missing To
		{Kind: DeleteEdge, From: "a", To: ""},
		{Kind: Kind(0), From: "a"}, // unknown kind
		{Kind: Kind(99), From: "a"},
	}
	for _, op := range bad {
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: want ErrInvalidInput, got %v", op, err)
		}
	}
	// Structural validation of the whole batch happens before any state change.
	_, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "ok"}, {Kind: Kind(0), From: "x"}}})
	if !errors.Is(err, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatalf("batch must be fully validated before applying: %v", err)
	}
	for _, name := range []string{"a", "z9", "a-b", "abcd"} {
		if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: name}}}); err != nil {
			t.Fatalf("valid name %q rejected: %v", name, err)
		}
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatalf("empty batch must not bump generation: %v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if r.Generation != 1 {
		t.Fatalf("generation=%d, want 1", r.Generation)
	}
	// Failed batch leaves generation untouched.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch changed generation")
	}
	// Multi-op successful batch bumps generation exactly once.
	r, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}}})
	if r.Generation != 2 {
		t.Fatalf("generation=%d, want 2", r.Generation)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"},
		{Kind: AddNode, From: "b"},
		{Kind: AddNode, From: "c"}, // exceeds MaxNodes at batch end
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("capacity failure must roll back the whole batch")
	}
	// Edge capacity exceeded mid-batch also rolls back.
	_, err = g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"},
		{Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "a", To: "b"},
		{Kind: AddEdge, From: "b", To: "a"}, // would be 2 edges > MaxEdges 1 (also a cycle)
	}})
	if err == nil || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatalf("rollback failed: %v", err)
	}
}

func TestErrorValues(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "ghost"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "a", To: "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	mustApply := func(ops ...Op) {
		t.Helper()
		if _, err := g.Apply(Batch{Ops: ops}); err != nil {
			t.Fatal(err)
		}
	}
	mustApply(Op{Kind: AddNode, From: "a"}, Op{Kind: AddNode, From: "b"})
	mustApply(Op{Kind: AddEdge, From: "a", To: "b"})
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatalf("self-loop must be ErrCycle, got %v", err)
	}
	if _, err := g.Reachable("a", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "BAD"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal("node must be reachable from itself")
	}
}

func TestDeleteNodeCascadeAndSnapshotOrder(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "a", To: "c"}, {Kind: AddEdge, From: "b", To: "c"}, {Kind: AddEdge, From: "a", To: "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatalf("nodes not sorted: %v", s.Nodes)
	}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Edges, wantEdges) {
		t.Fatalf("edges not sorted: %v", s.Edges)
	}
	// Returned slices must be isolated from internal state.
	s.Nodes[0] = "mutated"
	s.Edges[0] = Edge{"x", "y"}
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot shares memory with internal state")
	}
	// Deleting a node cascades to its incident edges.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "c"}}}); err != nil {
		t.Fatal(err)
	}
	s = g.Snapshot()
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}}) {
		t.Fatalf("cascade failed: %v", s.Edges)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g := mustGraph(t, Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	// Writers add disjoint nodes and chain edges; readers snapshot and query.
	for w := 0; w < 8; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := fmt.Sprintf("n%d", w)
			prev := ""
			for i := 0; i < 10; i++ {
				n := fmt.Sprintf("%s-%d", base, i)
				ops := []Op{{Kind: AddNode, From: n}}
				if prev != "" {
					ops = append(ops, Op{Kind: AddEdge, From: prev, To: n})
				}
				if _, err := g.Apply(Batch{Ops: ops}); err != nil {
					t.Error(err)
					return
				}
				prev = n
			}
		}()
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s := g.Snapshot()
				if len(s.Nodes) > 128 {
					t.Error("capacity violated")
					return
				}
				_, _ = g.Reachable("n0-0", "n0-9")
			}
		}()
	}
	wg.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 80 || len(s.Edges) != 72 {
		t.Fatalf("nodes=%d edges=%d", len(s.Nodes), len(s.Edges))
	}
	ok, err := g.Reachable("n3-0", "n3-9")
	if err != nil || !ok {
		t.Fatalf("chain unreachable: %v", err)
	}
	// Generation equals the number of successful non-empty batches.
	if s.Generation != 80 {
		t.Fatalf("generation=%d, want 80", s.Generation)
	}
}
