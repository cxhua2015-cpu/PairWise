package topologygraph228

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
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "b"},
		{AddEdge, "a", ""},
		{AddEdge, "a", "a"},
		{DeleteEdge, "a", "a"},
	}
	for i, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %d: %v", i, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %d: %v", i, err)
		}
	}
	// Validation must be side-effect free.
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Duplicate node mid-batch must roll back the whole batch.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if s := g.Snapshot(); s.Generation != before.Generation || len(s.Nodes) != 1 {
		t.Fatalf("not rolled back: %+v", s)
	}
	// Missing edge/node errors.
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	mk := func() {
		_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	}
	mk()
	// Net-zero batch exceeding capacity transiently must succeed.
	_, err := g.Apply(Batch{Ops: []Op{
		{DeleteNode, "a", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}, {DeleteNode, "d", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Net growth beyond capacity must fail and roll back.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "e", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := len(g.Snapshot().Nodes); got != 2 {
		t.Fatal(got)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatal(r.Generation)
	}
}

func TestReachableErrorsAndSelf(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("BAD", "a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
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
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 {
		t.Fatal("clone mutation leaked into original")
	}
	if c.Stats().Generation != g.Stats().Generation+1 {
		t.Fatal("generation not preserved/independent")
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
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 50; j++ {
				m := fmt.Sprintf("m%d-%d", i, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, m, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_ = g.Stats()
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, "x", ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, m, ""}}})
			}
		}()
	}
	for i := 0; i < 4; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 20; j++ {
				if c, err := g.Clone(); err == nil {
					_ = c.Snapshot()
				}
			}
		}()
	}
	w.Wait()
	s := g.Stats()
	if s.Nodes > 128 || s.Edges > 256 {
		t.Fatalf("capacity violated: %+v", s)
	}
	snap := g.Snapshot()
	if snap.Generation != s.Generation || len(snap.Nodes) != s.Nodes || len(snap.Edges) != s.Edges {
		t.Fatal("inconsistent snapshot vs stats")
	}
	for i := 1; i < len(snap.Nodes); i++ {
		if snap.Nodes[i-1] >= snap.Nodes[i] {
			t.Fatal("nodes not sorted")
		}
	}
	for i := 1; i < len(snap.Edges); i++ {
		a, b := snap.Edges[i-1], snap.Edges[i]
		if a.From > b.From || (a.From == b.From && a.To >= b.To) {
			t.Fatal("edges not sorted")
		}
	}
}
