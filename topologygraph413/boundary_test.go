package topologygraph413

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname"}
	for _, n := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	for _, n := range []string{"a", "a-b_c", "0", "12345678"} {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "a", "extra"},
		{DeleteNode, "a", "extra"},
		{AddEdge, "a", ""},
		{DeleteEdge, "", "b"},
		{AddEdge, "a", "a"},
	}
	for _, op := range cases {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	// ValidateBatch must not mutate state.
	if got := len(g.Snapshot().Nodes); got != 0 {
		t.Fatalf("ValidateBatch mutated state: %d nodes", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r1, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := g.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generation != r1.Generation || g.Stats().Generation != r1.Generation {
		t.Fatalf("empty batch bumped generation: %d -> %d", r1.Generation, r2.Generation)
	}
	if r1.Generation != 1 {
		t.Fatalf("first generation = %d", r1.Generation)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"},
		{AddNode, "a", ""}, // ErrExists mid-batch
	}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("rollback failed: %+v", got)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Peak of 3 nodes mid-batch, but final count is 2: must succeed.
	if _, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""},
	}}); err != nil {
		t.Fatal(err)
	}
	// Final count of 3 exceeds capacity: whole batch rolls back.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := len(g.Snapshot().Nodes); got != 2 {
		t.Fatalf("capacity rollback failed: %d nodes", got)
	}
}

func TestDeleteNodeCascadeAndErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if got := g.Stats(); got.Edges != 0 || got.Nodes != 2 {
		t.Fatalf("cascade failed: %+v", got)
	}
	for _, op := range []Op{
		{DeleteNode, "missing", ""},
		{AddEdge, "a", "missing"},
		{DeleteEdge, "a", "c"},
	} {
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "c"}, {AddEdge, "a", "c"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
}

func TestReachableErrorsAndSelf(t *testing.T) {
	g := graph(t)
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

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	for i := range wantNodes {
		if s.Nodes[i] != wantNodes[i] {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	for i := range wantEdges {
		if s.Edges[i] != wantEdges[i] {
			t.Fatalf("edges not sorted: %v", s.Edges)
		}
	}
	// Mutating the returned snapshot must not affect the graph.
	s.Nodes[0] = "zzz"
	s.Edges[0].From = "zzz"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIsolationAndClock(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != r.Generation {
		t.Fatal("clone lost logical clock")
	}
	// Both graphs evolve independently from the same generation.
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); err != nil {
		t.Fatal(err)
	}
	gs, cs := g.Stats(), c.Stats()
	if gs.Nodes != 3 || gs.Edges != 1 || cs.Nodes != 1 || cs.Edges != 0 {
		t.Fatalf("clone aliases original: g=%+v c=%+v", gs, cs)
	}
	if gs.Generation != cs.Generation {
		t.Fatalf("clocks diverged unexpectedly: %d vs %d", gs.Generation, cs.Generation)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("node-%d", i)
			m := fmt.Sprintf("node-%d", (i+1)%16)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
			_, _ = g.Reachable(n, m)
			_ = g.Snapshot()
			_ = g.Stats()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, "x", ""}}})
			if c, err := g.Clone(); err == nil {
				_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	wg.Wait()
	st := g.Stats()
	if st.Nodes != 16 {
		t.Fatalf("nodes = %d", st.Nodes)
	}
	snap := g.Snapshot()
	if snap.Generation != st.Generation || len(snap.Nodes) != st.Nodes || len(snap.Edges) != st.Edges {
		t.Fatalf("inconsistent snapshot vs stats: %+v vs %+v", snap, st)
	}
}
