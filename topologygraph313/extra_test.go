package topologygraph313

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "a/b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for _, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatal("invalid batch mutated state")
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("%+v: got %v want %v", b, e, want)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "b", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "b", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state not rolled back")
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal("failed batch mutated state")
	}
}

func TestEdgeCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatal("state not rolled back")
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
	if r.Generation != 2 {
		t.Fatal(r)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 2 {
		t.Fatal("failed batch changed generation")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}, {"c", "a"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot shares memory with internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("BAD", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				s := g.Snapshot()
				_ = s.Generation
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatal(s)
	}
}

func TestConcurrentChainBuilders(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			prev := fmt.Sprintf("n%02d", i*4)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, prev, ""}}})
			for j := 1; j < 4; j++ {
				n := fmt.Sprintf("n%02d", i*4+j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}, {AddEdge, prev, n}}})
				prev = n
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 || len(s.Edges) != 24 {
		t.Fatalf("nodes=%d edges=%d", len(s.Nodes), len(s.Edges))
	}
	ok, e := g.Reachable("n00", "n03")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}
