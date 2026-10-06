package topologygraph233

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
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Generation != 0 {
		t.Fatal("validation must not change generation")
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("generation drifted: %v", r)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestRollbackOnMissingNode(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "ghost"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("rollback failed")
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zzz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestReachableNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if fmt.Sprint(s.Nodes) != fmt.Sprint(wantNodes) || fmt.Sprint(s.Edges) != fmt.Sprint(wantEdges) {
		t.Fatalf("%+v", s)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
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
		t.Fatal("clone must preserve generation")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 || c.Stats().Nodes != 1 {
		t.Fatal("clone shares state with original")
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal("original edge index corrupted")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}, {AddEdge, n, "hub"}}})
			_, _ = g.Reachable(n, "hub")
			_ = g.Snapshot()
			_ = g.Stats()
			if i%3 == 0 {
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, "hub"}}})
			}
			if i%7 == 0 {
				_, _ = g.Clone()
			}
		}()
	}
	w.Wait()
	s := g.Stats()
	if s.Nodes != 33 {
		t.Fatalf("nodes=%d", s.Nodes)
	}
	if len(g.Snapshot().Edges) != s.Edges {
		t.Fatal("stats/snapshot inconsistent")
	}
}
