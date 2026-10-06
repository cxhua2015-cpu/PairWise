package topologygraph273

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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"}
	for _, n := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	for _, n := range []string{"a", "z-0_9", "abcdefgh"} {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralErrors(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind(0), "a", ""}}},
		{Ops: []Op{{Kind(99), "a", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "", "b"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{}); err != nil {
		t.Fatal(err)
	}
}

func TestStateErrorsAndRollback(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, err := g.Apply(b); !errors.Is(err, want) {
			t.Fatalf("want %v got %v", want, err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	// rollback: second op fails, first op must not persist
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}}, ErrExists)
	if len(g.Snapshot().Nodes) != 1 {
		t.Fatal("rollback leaked node b")
	}
	// duplicate edge
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
}

func TestCapacityRollback(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// exceeds node capacity at batch end -> full rollback
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity rollback leaked %d nodes", n)
	}
	// exceeds edge capacity at batch end -> full rollback
	g2, err := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	_, err = g2.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "c"}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if s := g2.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatal("capacity rollback leaked state")
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	if g.Stats().Generation != 0 {
		t.Fatal()
	}
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatalf("non-empty batch must bump generation once, got %d", r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if g.Stats().Generation != 2 {
		t.Fatal("failed batch changed generation")
	}
	// clone preserves the logical clock
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != 2 {
		t.Fatal("clone lost generation")
	}
}

func TestDeleteNodeCascadeAndReachable(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := g.Reachable("a", "c"); !ok {
		t.Fatal()
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("cascade left %+v", s.Edges)
	}
	// self reachability of an existing node
	if ok, _ := g.Reachable("a", "a"); !ok {
		t.Fatal()
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	for i, e := range wantE {
		if s.Edges[i] != e {
			t.Fatalf("edges not sorted: %v", s.Edges)
		}
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("node-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "node-0"}}})
			_ = g.Stats()
			_ = g.Snapshot()
			_, _ = g.Reachable(n, n)
			if i%4 == 0 {
				_, _ = g.Clone()
			}
		}()
	}
	w.Wait()
	if got := g.Stats().Nodes; got != 16 {
		t.Fatal(got)
	}
	// concurrent edge adds from distinct sources into a shared sink
	var w2 sync.WaitGroup
	for i := 1; i < 16; i++ {
		i := i
		w2.Add(1)
		go func() {
			defer w2.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, fmt.Sprintf("node-%d", i), "node-0"}}})
		}()
	}
	w2.Wait()
	if got := g.Stats().Edges; got != 15 {
		t.Fatal(got)
	}
	if ok, _ := g.Reachable("node-7", "node-0"); !ok {
		t.Fatal()
	}
}
