package controlgraph118

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
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
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation changed on invalid input")
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, e = g.Apply(Batch{})
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
	must(Batch{Ops: []Op{{DeleteNode, "b", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "b", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "a"}}}, ErrCycle)
}

func TestRollbackAtomicity(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "c"}, {AddEdge, "c", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("state mutated: %+v", got)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadeAndCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}, {AddEdge, "a", "c"}, {AddEdge, "c", "d"}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 2 || len(s.Edges) != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatal(s.Nodes)
		}
	}
	wantEdges := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	for i, ed := range wantEdges {
		if s.Edges[i] != ed {
			t.Fatal(s.Edges)
		}
	}
	s.Nodes[0] = "zzz"
	s.Edges[0] = Edge{"z", "z"}
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n-%02d", i)
			m := fmt.Sprintf("n-%02d", (i+1)%32)
			for k := 0; k < 20; k++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 128 || len(s.Edges) > 256 {
		t.Fatal("capacity violated")
	}
}
