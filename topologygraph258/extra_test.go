package topologygraph258

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

func TestValidateBatchStructural(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{DeleteNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	valid := Batch{Ops: []Op{{AddNode, "a-1_b", ""}, {AddEdge, "a-1_b", "c"}}}
	if err := g.ValidateBatch(valid); err != nil {
		t.Fatal(err)
	}
	// ValidateBatch must not mutate state.
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 || s.Generation != 0 {
		t.Fatalf("validate mutated state: %+v", s)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("generation moved: %+v", r)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {DeleteNode, "ghost", ""}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s := g.Snapshot(); s.Generation != before.Generation || len(s.Nodes) != 0 {
		t.Fatalf("state leaked: %+v", s)
	}
}

func TestCapacityRollbackAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("rollback failed, nodes=%d", n)
	}
	// Temporary overflow inside the batch is fine: only final counts matter.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if e := len(g.Snapshot().Edges); e != 0 {
		t.Fatalf("cycle rollback failed, edges=%d", e)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
		{DeleteNode, "b", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("%+v", s)
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
	if _, err := g.Reachable("a", "ghost"); !errors.Is(err, ErrNotFound) {
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
			t.Fatalf("order: %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[2] != (Edge{"b", "c"}) {
		t.Fatalf("edge order: %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	r, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != r.Generation {
		t.Fatal("clone lost logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 {
		t.Fatal("clone write leaked into original")
	}
	if c.Stats().Edges != 0 {
		t.Fatal("cascade missing on clone")
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
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			_ = g.Stats()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
			if i%4 == 0 {
				c, err := g.Clone()
				if err == nil {
					_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				}
			}
		}()
	}
	w.Wait()
	if got := g.Stats().Nodes; got != 32 {
		t.Fatalf("nodes=%d", got)
	}
	if g.Stats().Generation != 32 {
		t.Fatalf("generation=%d", g.Stats().Generation)
	}
}

func TestConcurrentEdgeContention(t *testing.T) {
	g, _ := New(Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
		}()
	}
	w.Wait()
	if g.Stats().Edges != 1 {
		t.Fatalf("edges=%d", g.Stats().Edges)
	}
}
