package topologygraph273

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
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); err != ErrInvalidInput {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{}); err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Nodes != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %v %+v", err, r)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Generation != 2 {
		t.Fatalf("generation: %+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "missing", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Generation != 2 {
		t.Fatalf("failed batch bumped generation: %+v", s)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("batch not rolled back: %d nodes", n)
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
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); err != ErrExists {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}})
	if z := g.Stats(); z.Edges != 0 {
		t.Fatalf("%+v", z)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i := range wantN {
		if s.Nodes[i] != wantN[i] {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	for i := 1; i < len(s.Edges); i++ {
		p, q := s.Edges[i-1], s.Edges[i]
		if p.From > q.From || (p.From == q.From && p.To >= q.To) {
			t.Fatalf("edges not sorted: %v", s.Edges)
		}
	}
	s.Nodes[0] = "mutated"
	s.Edges[0] = Edge{"x", "y"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestCloneIndependence(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone diverges at birth")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || c.Stats().Nodes != 1 {
		t.Fatal("clone shares ownership")
	}
	if c.Stats().Generation != 2 {
		t.Fatal("clone did not preserve logical clock")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
			_ = g.Snapshot()
			_ = g.Stats()
			_, _ = g.Reachable(n, n)
			_, _ = g.Clone()
		}()
	}
	w.Wait()
	if z := g.Stats(); z.Nodes != 16 || z.Generation != 16 {
		t.Fatalf("%+v", z)
	}
}
