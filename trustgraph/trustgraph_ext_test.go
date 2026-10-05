package trustgraph

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind: 0, From: "a"},                   // unknown kind
		{Kind: 99, From: "a"},                  // unknown kind
		{Kind: AddNode, From: ""},              // empty name
		{Kind: AddNode, From: "A"},             // uppercase
		{Kind: AddNode, From: "a b"},           // space
		{Kind: AddNode, From: "a/b"},           // slash
		{Kind: AddNode, From: "toolongname"},   // over MaxNameBytes=8
		{Kind: AddNode, From: "a", To: "b"},    // extra field
		{Kind: DeleteNode, From: "a", To: "b"}, // extra field
		{Kind: AddEdge, From: "a"},             // missing to
		{Kind: DeleteEdge, To: "b"},            // missing from
		{Kind: AddEdge, From: "a", To: "Bad"},  // invalid to
	}
	for _, op := range cases {
		_, err := g.Apply(Batch{Ops: []Op{op}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: got %v", op, err)
		}
	}
	// Whole batch validated before state is read: second invalid op must
	// prevent the first (valid) op from being applied.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", "x"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("batch must be fully validated before applying")
	}
}

func TestValidNamesAccepted(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a-z0_9", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	must := func(b Batch) {
		t.Helper()
		if _, err := g.Apply(b); err != nil {
			t.Fatal(err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatalf("state must roll back: %+v", got)
	}
	// Edge capacity checked at batch end too.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestRollbackOnSemanticError(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// First op succeeds, second fails: nothing may be applied.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {DeleteNode, "zz", ""}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 1 {
		t.Fatalf("state must roll back: %+v", got)
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatalf("empty batch must not bump generation: %+v", r)
	}
	r, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if err != nil || r.Generation != 1 {
		t.Fatalf("got %+v, %v", r, err)
	}
	// Failed batch must not bump generation.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
	// DeleteNode with incident edges is still one non-empty batch: +1.
	r, err = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "b", ""}}})
	if err != nil || r.Generation != 2 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
		{DeleteNode, "b", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 2 || len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("cascade failed: %+v", s)
	}
	// Deleting b must free the cycle it participated in.
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	wantEdges := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if fmt.Sprint(s.Nodes) != fmt.Sprint(wantNodes) || fmt.Sprint(s.Edges) != fmt.Sprint(wantEdges) {
		t.Fatalf("not sorted: %+v", s)
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := g.Reachable("a", "a"); !ok || err != nil {
		t.Fatal("self must be reachable")
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("zz", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("Bad", "a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	wg.Wait()
	if got := len(g.Snapshot().Nodes); got != 0 {
		t.Fatalf("expected empty graph, got %d nodes", got)
	}
}

func TestConcurrentDAGInvariant(t *testing.T) {
	// Concurrent writers add edges in both directions along a fixed node
	// order; whatever succeeds, the committed graph must stay acyclic and
	// generation must equal the number of successful non-empty batches.
	g, _ := New(Options{MaxNodes: 32, MaxEdges: 256, MaxNameBytes: 8})
	names := []string{"a", "b", "c", "d", "e", "f"}
	var ops []Op
	for _, n := range names {
		ops = append(ops, Op{AddNode, n, ""})
	}
	if _, err := g.Apply(Batch{Ops: ops}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < len(names); i++ {
		for j := 0; j < len(names); j++ {
			if i == j {
				continue
			}
			i, j := i, j
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := g.Apply(Batch{Ops: []Op{{AddEdge, names[i], names[j]}}})
				mu.Lock()
				if err == nil {
					successes++
				}
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	s := g.Snapshot()
	if int(s.Generation) != 1+successes {
		t.Fatalf("generation %d != 1+%d", s.Generation, successes)
	}
	// Kahn's algorithm on the snapshot to verify acyclicity.
	indeg := map[string]int{}
	out := map[string][]string{}
	for _, n := range s.Nodes {
		indeg[n] = 0
	}
	for _, e := range s.Edges {
		indeg[e.To]++
		out[e.From] = append(out[e.From], e.To)
	}
	var queue []string
	for n, d := range indeg {
		if d == 0 {
			queue = append(queue, n)
		}
	}
	visited := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		visited++
		for _, m := range out[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if visited != len(s.Nodes) {
		t.Fatal("committed graph contains a cycle")
	}
}
