package topologygraph293

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
	for _, n := range []string{"a", "node-1", "n_2", "12345678"} {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind(0), "a", ""},
		{Kind(99), "a", ""},
		{AddNode, "a", "extra"},
		{DeleteNode, "a", "extra"},
		{AddEdge, "a", ""},
		{AddEdge, "a", "a"},
		{DeleteEdge, "a", "a"},
	}
	for _, op := range cases {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
}

func TestValidateBatchHasNoSideEffects(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatalf("validation mutated state: %+v", got)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		_, err := g.Apply(b)
		if !errors.Is(err, want) {
			t.Fatalf("want %v got %v", want, err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}, ErrExists)
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {DeleteNode, "zz", ""}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 1 {
		t.Fatalf("rollback failed: %+v", got)
	}
}

func TestCapacityRollbackAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 2 {
		t.Fatalf("capacity rollback failed: %+v", got)
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if r.Generation != 1 {
		t.Fatalf("generation: %d", r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if g.Stats().Generation != 1 {
		t.Fatalf("failed batch bumped generation: %d", g.Stats().Generation)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[1] != (Edge{"c", "a"}) {
		t.Fatalf("edges not sorted: %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0] = Edge{"x", "y"}
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("self reachability: %t %v", ok, err)
	}
}

func TestCloneIsolationAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("clone lost logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 1 || len(c.Snapshot().Edges) != 0 {
		t.Fatal("clone shares ownership with original")
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
			n := fmt.Sprintf("node-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_ = g.Stats()
			}
		}()
	}
	wg.Wait()
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Snapshot().Nodes); got != 16 {
		t.Fatalf("nodes: %d", got)
	}
}

func TestConcurrentCloneAndApply(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, string(rune('a' + i)), ""}}})
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := g.Clone(); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if len(g.Snapshot().Nodes) != 8 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}
