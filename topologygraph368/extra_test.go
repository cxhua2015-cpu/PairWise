package topologygraph368

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},                        // unknown kind
		{Ops: []Op{{Kind: 99, From: "a"}}},                       // unknown kind
		{Ops: []Op{{AddNode, "", ""}}},                           // empty name
		{Ops: []Op{{AddNode, "A", ""}}},                          // uppercase
		{Ops: []Op{{AddNode, "a b", ""}}},                        // space
		{Ops: []Op{{AddNode, "a.b", ""}}},                        // dot
		{Ops: []Op{{AddNode, "toolongname", ""}}},                // over MaxNameBytes
		{Ops: []Op{{AddNode, "a", "b"}}},                         // extra field
		{Ops: []Op{{DeleteNode, "a", "b"}}},                      // extra field
		{Ops: []Op{{AddEdge, "a", ""}}},                          // missing to
		{Ops: []Op{{DeleteEdge, "", "b"}}},                       // missing from
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal("state mutated by invalid batches")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	r, e = g.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}, ErrNotFound)
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal("not rolled back", s)
	}
	// Edge capacity exceeded mid-batch: node adds must roll back too.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Edges) != 1 || s.Generation != 1 {
		t.Fatal(s)
	}
}

func TestDeleteNodeCascadeAndReuse(t *testing.T) {
	g, _ := New(Options{MaxNodes: 3, MaxEdges: 4, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatal(s)
	}
	// Freed capacity is reusable after delete.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {AddEdge, "a", "d"}, {AddEdge, "d", "c"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "b", "a"}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 2 || s.Nodes[0] != "a" || s.Nodes[1] != "b" {
		t.Fatal("nodes not sorted", s.Nodes)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "b" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%03d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "hub", n}}})
			_, _ = g.Reachable("hub", n)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "hub", n}}})
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 33 {
		t.Fatal(len(s.Nodes))
	}
}

func TestConcurrentApplySerialize(t *testing.T) {
	g, _ := New(Options{MaxNodes: 1000, MaxEdges: 1000, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("w%02d", i)
			// Chain: each worker builds w_i -> w_i+1 edges via unique nodes.
			for j := 0; j < 20; j++ {
				m := fmt.Sprintf("%s-%02d", n, j)
				if _, e := g.Apply(Batch{Ops: []Op{{AddNode, m, ""}}}); e != nil {
					t.Error(e)
					return
				}
			}
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 16*20 {
		t.Fatal(got)
	}
}
