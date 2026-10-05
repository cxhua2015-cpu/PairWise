package controlgraph138

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
	cases := []Op{
		{AddNode, "", ""},
		{AddNode, "Upper", ""},
		{AddNode, "has space", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "ok-name", "extra"},
		{AddEdge, "a", ""},
		{AddEdge, "", "b"},
		{Kind(0), "a", ""},
		{Kind(99), "a", ""},
	}
	for _, op := range cases {
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "ok-n_1", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRollbackAtomicity(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "nope", ""}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("state leaked: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	// Edge capacity exceeded only at batch end.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) && !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "b", "a"}, {AddEdge, "a", "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatalf("nodes %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "c"}) || s.Edges[1] != (Edge{"b", "a"}) {
		t.Fatalf("edges %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(4, []string{"w"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("n-%d", i)
			_, _ = c.Apply("w", Batch{Ops: []Op{{AddNode, name, ""}}})
			_, _ = c.Apply("w", Batch{Ops: []Op{{AddNode, "x" + name, ""}, {AddEdge, name, "x" + name}}})
			_, _ = g.Reachable(name, "x"+name)
			_ = g.Snapshot()
			_ = c.Decisions()
		}()
	}
	wg.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
	if got := len(c.Decisions()); got != 32 {
		t.Fatal(got)
	}
}

func TestConcurrentPolicyReplace(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 16})
	p, _ := NewPolicy(2, []string{"a"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := fmt.Sprintf("actor-%d", i%3)
			_ = p.ReplaceActors([]string{actor})
			_, _ = c.Apply(actor, Batch{Ops: []Op{{AddNode, fmt.Sprintf("n%d", i), ""}}})
			_ = p.Authorize(actor, 1)
		}()
	}
	wg.Wait()
}

func TestDecisionSequencesMonotonic(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 16})
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "a"
			if i%2 == 0 {
				actor = "denied"
			}
			_, _ = c.Apply(actor, Batch{Ops: []Op{{AddNode, fmt.Sprintf("n%d", i), ""}}})
		}()
	}
	wg.Wait()
	ds := c.Decisions()
	if len(ds) != 10 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %+v", i, ds)
		}
	}
}
