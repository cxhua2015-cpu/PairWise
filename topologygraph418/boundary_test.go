package topologygraph418

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
		{AddNode, "a", "extra"},
		{DeleteNode, "a", "extra"},
		{AddEdge, "a", ""},
		{AddEdge, "", "b"},
		{AddEdge, "a", "a"},
		{DeleteEdge, "a", "a"},
	}
	for i, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %d %+v: %v", i, op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	// ValidateBatch must not mutate state.
	if s := g.Stats(); s.Nodes != 0 || s.Edges != 0 || s.Generation != 0 {
		t.Fatalf("validation had side effects: %+v", s)
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
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("%v %v", r, err)
	}
}

func TestBatchRollbackOnError(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"},
		{DeleteEdge, "a", "zzz"},
	}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestCapacityRollbackAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity rollback failed: %d nodes", n)
	}
	// Edge capacity: transient overflow inside the batch is fine, final is not.
	_, err = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "a", "b"}, {DeleteEdge, "a", "b"}, {AddEdge, "b", "a"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) && !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if e := len(g.Snapshot().Edges); e != 1 {
		t.Fatalf("edges after failed batch: %d", e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("nope", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("BAD!", "a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("self reachability: %v %v", ok, err)
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	r, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats().Generation; got != r.Generation {
		t.Fatalf("clone lost logical clock: %d != %d", got, r.Generation)
	}
	// Mutating the clone must not affect the original, and vice versa.
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "z", ""}}})
	if len(g.Snapshot().Edges) != 1 || len(c.Snapshot().Edges) != 0 {
		t.Fatal("clone aliases original")
	}
	if len(g.Snapshot().Nodes) != 3 || len(c.Snapshot().Nodes) != 1 {
		t.Fatal("clone aliases original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("node-%d", i)
			for j := 0; j < 50; j++ {
				m := fmt.Sprintf("n-%d-%d", i, j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, m, ""}, {AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_ = g.Stats()
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, m, n}}})
				if j%10 == 0 {
					_, _ = g.Clone()
				}
			}
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
		}()
	}
	w.Wait()
	s := g.Stats()
	snap := g.Snapshot()
	if s.Nodes != len(snap.Nodes) || s.Edges != len(snap.Edges) || s.Generation != snap.Generation {
		t.Fatalf("inconsistent views: %+v vs gen=%d", s, snap.Generation)
	}
}
