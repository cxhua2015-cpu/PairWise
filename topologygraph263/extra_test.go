package topologygraph263

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "a", "extra"},
		{AddNode, "toolongname", ""},
		{AddEdge, "a", ""},
		{AddEdge, "", "b"},
		{AddEdge, "a", "a"},
		{DeleteEdge, "a", "a"},
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	// Validation must be side-effect free: nothing was added.
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Stats().Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("generation after empty batch: %d", r.Generation)
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "missing", ""},
	}})
	if err != ErrNotFound {
		t.Fatal(err)
	}
	s := g.Stats()
	if s.Nodes != 0 || s.Edges != 0 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity failure must roll back, got %d nodes", n)
	}
	// Delete-then-add within one batch only checks final capacity.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if got := g.Snapshot().Nodes; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("state after rollback: %v", got)
	}
}

func TestStateErrors(t *testing.T) {
	g := graph(t)
	must := func(want error, ops ...Op) {
		t.Helper()
		if _, err := g.Apply(Batch{Ops: ops}); !errors.Is(err, want) {
			t.Fatalf("ops %v: want %v got %v", ops, want, err)
		}
	}
	must(ErrNotFound, Op{DeleteNode, "ghost", ""})
	must(ErrNotFound, Op{AddEdge, "ghost", "x"})
	must(ErrNotFound, Op{DeleteEdge, "a", "b"})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	must(ErrExists, Op{AddNode, "a", ""})
	must(ErrNotFound, Op{AddEdge, "a", "ghost"})
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	must(ErrExists, Op{AddEdge, "a", "b"})
	if _, err := g.Reachable("a", "ghost"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestDeleteNodeCascadeAndCycle(t *testing.T) {
	g := graph(t)
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
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("cascade: %+v", s)
	}
	// After deleting b, c->a is still a cycle via a->c.
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); err != ErrCycle {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "b", ""}, {AddNode, "a", ""}, {AddNode, "c", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatalf("node order: %v", s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i, e := range wantE {
		if s.Edges[i] != e {
			t.Fatalf("edge order: %v", s.Edges)
		}
	}
	// Mutating returned slices must not affect the graph.
	s.Nodes[0] = "zzz"
	s.Edges[0] = Edge{"z", "z"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("clone must preserve logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 || c.Stats().Nodes != 1 {
		t.Fatal("clone and original must be fully independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, fmt.Sprintf("n-%02d", (i+1)%16)}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.Stats()
				_ = g.Snapshot()
				_, _ = g.Reachable(n, n)
				if j%5 == 0 {
					_, _ = g.Clone()
				}
			}
		}()
	}
	wg.Wait()
	s := g.Stats()
	if s.Nodes > 16 || s.Edges > 256 {
		t.Fatalf("inconsistent stats: %+v", s)
	}
	snap := g.Snapshot()
	if snap.Generation != s.Generation || len(snap.Nodes) != s.Nodes || len(snap.Edges) != s.Edges {
		t.Fatal("stats and snapshot disagree")
	}
}
