package topologygraph438

import (
	"errors"
	"fmt"
	"reflect"
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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != ErrInvalidInput {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	for _, n := range []string{"a", "z-0_9", "12345678"} {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{Kind: 99, From: "a"}}}); err != ErrInvalidInput {
		t.Fatal(err)
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "a", "extra"}}}); err != ErrInvalidInput {
		t.Fatal(err)
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddEdge, "a", ""}}}); err != ErrInvalidInput {
		t.Fatal(err)
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatalf("not rolled back: %+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "x", "y"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 4})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	g2, _ := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 4})
	if _, err := g2.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "c"}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if s := g2.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatalf("not rolled back: %+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if g.Stats().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "a"}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}, {"c", "a"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone lost logical clock")
	}
	r, _ := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if r.Generation != 2 {
		t.Fatal(r.Generation)
	}
	if g.Stats().Generation != 1 || len(g.Snapshot().Edges) != 1 {
		t.Fatal("clone mutated original")
	}
}

func TestPreviewErrorParity(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	cases := []Batch{
		{Ops: []Op{{AddEdge, "b", "a"}}},
		{Ops: []Op{{AddNode, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "b", "a"}}},
		{Ops: []Op{{Kind: 42, From: "a"}}},
		{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}},
	}
	for _, b := range cases {
		_, _, _, perr := g.Preview(b)
		c, _ := g.Clone()
		_, aerr := c.Apply(b)
		if !errors.Is(perr, aerr) || (perr == nil) != (aerr == nil) {
			t.Fatalf("preview=%v apply=%v", perr, aerr)
		}
	}
	if g.Stats().Generation != 1 {
		t.Fatal("preview bumped generation")
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
			n := fmt.Sprintf("node-%d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.Snapshot()
				_ = g.Stats()
				_, _ = g.Reachable(n, n)
				_, _, _, _ = g.Preview(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Clone()
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, "x", "y"}}})
			}
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
		}()
	}
	w.Wait()
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 {
		t.Fatalf("%+v", s)
	}
}
