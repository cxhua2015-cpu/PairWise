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
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "a/b", ""},
		{AddNode, "toolongname", ""}, // > MaxNameBytes=8
		{AddNode, "a", "extra"},      // extra field on node op
		{DeleteNode, "a", "b"},
		{AddEdge, "a", ""},
		{AddEdge, "", "b"},
		{DeleteEdge, "a", "Bad"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	// Structural validation happens before state reads: unknown kind must
	// win even when an earlier op would fail against state.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 42, From: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestValidNameCharset(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a-z_0", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "x", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "x"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "x", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestRollbackAtomic(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	// Batch fails mid-way on cycle; earlier ops must not commit.
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"},
	}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatal("failed batch leaked state")
	}
}

func TestCapacityEndOnly(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	// Peak of 3 nodes mid-batch, final 2: allowed.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	// Final 3 nodes: rejected and rolled back.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("capacity failure leaked state")
	}
	// Edge capacity.
	g2, _ := New(Options{MaxNodes: 4, MaxEdges: 2, MaxNameBytes: 8})
	_, e = g2.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g2.Snapshot().Edges) != 0 {
		t.Fatal("capacity failure leaked edges")
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	if g.Snapshot().Generation != 0 {
		t.Fatal()
	}
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch leaves generation unchanged.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if e != nil || r.Generation != 2 {
		t.Fatal(r, e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatal(s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i, e := range wantE {
		if s.Edges[i] != e {
			t.Fatal(s.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"z", "z"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot shares state")
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
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
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
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			for j := 0; j < 16; j++ {
				m := fmt.Sprintf("n%02d", j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 16 {
		t.Fatal(len(s.Nodes))
	}
	// Graph must remain acyclic after the concurrent storm.
	for _, e := range s.Edges {
		ok, err := g.Reachable(e.To, e.From)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("cycle through %v", e)
		}
	}
}
