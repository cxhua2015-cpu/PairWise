package controlgraph113

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -2, 1}, {1, 1, -3},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, e)
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
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{DeleteNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation changed by invalid batches")
	}
}

func TestValidNameCharset(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a-0_b", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r, _ = g.Apply(Batch{Ops: nil})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		_, e := g.Apply(b)
		if !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "zz"}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatal(s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteNode, "c", ""}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {DeleteNode, "b", ""},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatal(s)
	}
	if _, e := g.Reachable("a", "c"); e != nil {
		t.Fatal(e)
	}
}

func TestReachableNotFound(t *testing.T) {
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

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
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
			n := fmt.Sprintf("n-%d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, fmt.Sprintf("n-%d", (i+1)%16)}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 128 || len(s.Edges) > 256 {
		t.Fatal("capacity violated")
	}
}
