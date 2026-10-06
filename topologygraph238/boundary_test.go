package topologygraph238

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
	bad := []Op{
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "a.b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "extra"},
		{AddEdge, "a", "a"},
		{Kind(0), "a", ""},
		{Kind(99), "a", ""},
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok_nm-1", ""}}}); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot().Generation; got != 0 {
		t.Fatalf("failed batches bumped generation to %d", got)
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
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"},
	}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("partial state leaked: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity failure leaked %d nodes", n)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	g := graph(t)
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
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("%+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "c"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("nope", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("bad name", "a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotOwnershipAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "b", ""}, {AddNode, "a", ""}, {AddNode, "c", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatalf("nodes %v", s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i, e := range wantE {
		if s.Edges[i] != e {
			t.Fatalf("edges %v", s.Edges)
		}
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
	if z := c.Stats(); z.Generation != r.Generation || z.Nodes != 2 || z.Edges != 1 {
		t.Fatalf("%+v", z)
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Reachable("a", "b"); !ok {
		t.Fatal("clone mutation leaked into original")
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clone should have diverged: %v", err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("node-%02d", i)
			for k := 0; k < 20; k++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
				_ = g.Snapshot()
				_ = g.Stats()
				_, _ = g.Reachable("a", "b")
				if c, err := g.Clone(); err == nil {
					_, _ = c.Apply(Batch{Ops: []Op{{AddNode, "scratch", ""}}})
				}
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	st := g.Stats()
	if s.Generation != st.Generation {
		t.Fatalf("inconsistent: %+v", s)
	}
	if len(s.Nodes) != st.Nodes || len(s.Edges) != st.Edges {
		t.Fatal("stats disagree with snapshot")
	}
}
