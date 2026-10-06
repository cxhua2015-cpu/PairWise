package topologygraph223

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsAndNameValidation(t *testing.T) {
	if _, err := New(Options{0, 1, 1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{1, 1, -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "extra"},
		{AddEdge, "a", ""},
		{AddEdge, "a", "a"},
		{DeleteEdge, "ok", "BAD"},
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_2", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r0, err := g.Apply(Batch{})
	if err != nil || r0.Generation != 0 {
		t.Fatalf("%+v %v", r0, err)
	}
	r1, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if err != nil || r1.Generation != 1 {
		t.Fatalf("%+v %v", r1, err)
	}
	r2, err := g.Apply(Batch{})
	if err != nil || r2.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("%+v %v", r2, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatalf("not rolled back: %+v", got)
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("cycle failure leaked edges")
	}
}

func TestDeleteNodeCascadeAndNotFound(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Edges != 0 || s.Nodes != 2 {
		t.Fatalf("%+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "c"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "a"}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	want := []string{"a", "b", "c"}
	for i, n := range want {
		if s.Nodes[i] != n {
			t.Fatalf("nodes %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[1] != (Edge{"c", "a"}) {
		t.Fatalf("edges %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneClockAndIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("clone lost logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Edges != 1 || c.Stats().Edges != 0 {
		t.Fatal("clone aliases source")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 12})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n-%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
			_ = g.Stats()
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			if j := i - 1; j >= 0 {
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, fmt.Sprintf("n-%02d", j), n}}})
			}
			_, _ = g.Clone()
		}()
	}
	wg.Wait()
	s := g.Stats()
	if s.Nodes != 16 || int(s.Generation) > 31 {
		t.Fatalf("%+v", s)
	}
	snap := g.Snapshot()
	if snap.Generation != s.Generation || len(snap.Nodes) != 16 {
		t.Fatal("inconsistent snapshot")
	}
}
