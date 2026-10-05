package controlgraph083

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
			t.Fatalf("options %+v: got %v", o, e)
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
		{Ops: []Op{{AddEdge, "", "b"}}},
		{Ops: []Op{{DeleteEdge, "a", "B"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: got %v", i, e)
		}
	}
	// Structural validation happens before state reads: an invalid op
	// anywhere in the batch must fail even if earlier ops would succeed.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok", ""}, {AddNode, "BAD", ""}}}); !errors.Is(e, ErrInvalidInput) {
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
			t.Fatalf("got %v want %v", e, want)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "b", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "b", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestCycleDetection(t *testing.T) {
	g := graph(t)
	ops := []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "c"}}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	for _, e2 := range []Edge{{"a", "a"}, {"c", "a"}, {"c", "b"}, {"b", "a"}} {
		if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, e2.From, e2.To}}}); !errors.Is(err, ErrCycle) {
			t.Fatalf("edge %v: got %v", e2, err)
		}
	}
	// Cycle failure inside a batch rolls back the whole batch.
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {AddEdge, "c", "a"}}})
	if !errors.Is(e, ErrCycle) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	ops := []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("edges=%v", s.Edges)
	}
	if ok, _ := g.Reachable("a", "c"); !ok {
		t.Fatal()
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); e != nil {
		t.Fatal(e)
	}
	// Edge capacity exceeded at batch end: rollback.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "a", "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Edges); n != 0 {
		t.Fatal(n)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %v %v", r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if r.Generation != 2 {
		t.Fatal(r.Generation)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 2 {
		t.Fatal("failed batch changed generation")
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 2 {
		t.Fatal("empty batch changed generation")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	ops := []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
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
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal(g.Snapshot().Nodes)
	}
}
