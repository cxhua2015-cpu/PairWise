package topologygraph383

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxNodes: 0, MaxEdges: 1, MaxNameBytes: 1},
		{MaxNodes: 1, MaxEdges: 0, MaxNameBytes: 1},
		{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: 0},
		{MaxNodes: -1, MaxEdges: 1, MaxNameBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: Kind(99), From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "extra"},
		{AddEdge, "a", ""},
		{DeleteEdge, "", "b"},
		{DeleteNode, "a", "extra"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch) {
		t.Helper()
		if _, e := g.Apply(b); e != nil {
			t.Fatal(e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("A", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRollbackAtomic(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatal(got)
	}
	// Failed op mid-batch rolls back earlier ops.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {DeleteNode, "zz", ""}}})
	if !errors.Is(e, ErrNotFound) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if s.Nodes[0] != "a" || s.Nodes[1] != "b" {
		t.Fatal(s.Nodes)
	}
	s.Nodes[0] = "mut"
	s.Edges[0].From = "mut"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal(again)
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
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
}

func TestConcurrentEdges(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 10; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
				_, _ = g.Reachable("a", "b")
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Edges) > 1 {
		t.Fatal(s.Edges)
	}
}
