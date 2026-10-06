package topologygraph293

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("%+v: %v", o, err)
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
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range bad {
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); err != ErrInvalidInput {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatal(r.Generation)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// Batch fails midway (duplicate node), nothing should be committed.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "c", "b"}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	after := g.Snapshot()
	if before.Generation != after.Generation || len(after.Nodes) != 2 || len(after.Edges) != 1 {
		t.Fatalf("no rollback: %+v", after)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 4})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 || s.Generation != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestDeleteNodeCascades(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"}}})
	_, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Edges != 0 {
		t.Fatalf("%+v", s)
	}
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestNotFoundAndExists(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "x", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "x", "y"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "x", "y"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "x", ""}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "x", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
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
		t.Fatal("clock not preserved")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "z", ""}}})
	if len(c.Snapshot().Nodes) != 1 || len(g.Snapshot().Nodes) != 3 {
		t.Fatal("clone aliases original")
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
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "hub"}}})
			_, _ = g.Reachable(n, "hub")
			_ = g.Stats()
			c, _ := g.Clone()
			_ = c.Snapshot()
			if i%2 == 0 {
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}})
	w.Wait()
	s := g.Snapshot()
	st := g.Stats()
	if len(s.Nodes) != st.Nodes || len(s.Edges) != st.Edges || s.Generation != st.Generation {
		t.Fatal("inconsistent snapshot/stats")
	}
}
