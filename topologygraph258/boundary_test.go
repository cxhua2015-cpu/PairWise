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

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	bad := []Batch{
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
	for i, b := range bad {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if s := g.Stats(); s.Generation != 0 || s.Nodes != 0 || s.Edges != 0 {
		t.Fatalf("failed validation mutated state: %+v", s)
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatalf("gen=%d", r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if g.Stats().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestBatchRollback(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteEdge, "a", "b"}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || fmt.Sprint(got.Nodes) != fmt.Sprint(before.Nodes) || fmt.Sprint(got.Edges) != fmt.Sprint(before.Edges) {
		t.Fatal("state changed after rolled-back batch")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {DeleteNode, "a", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 2 {
		t.Fatal(n)
	}
}

func TestDeleteNodeCascadeAndMissing(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Edges); n != 0 {
		t.Fatal("cascade failed")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Reachable("a", "BAD"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if s.Nodes[0] != "a" || s.Nodes[1] != "b" {
		t.Fatal("nodes not sorted")
	}
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{From: "zz", To: "zz"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependence(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone diverges")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || c.Stats().Nodes != 1 {
		t.Fatal("clone shares state")
	}
	if g.Stats().Generation != c.Stats().Generation+1-1 && g.Stats().Generation != 1 {
		t.Fatal("generation not preserved")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := string(rune('a' + i))
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_ = g.Stats()
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "zz"}}})
			}
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
		}()
	}
	w.Wait()
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 {
		t.Fatalf("leaked state: %+v", s)
	}
}

func TestConcurrentCloneAndApply(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 30; j++ {
				c, err := g.Clone()
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	w.Wait()
	if s := g.Stats(); s.Nodes != 2 || s.Edges != 1 || s.Generation != 1 {
		t.Fatalf("original mutated: %+v", s)
	}
}
