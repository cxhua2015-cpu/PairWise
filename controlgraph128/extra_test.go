package controlgraph128

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -2, 1}, {1, 1, -3},
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
		{Ops: []Op{{AddEdge, "", "b"}}},
		{Ops: []Op{{DeleteEdge, "a", "B"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if _, e := g.Reachable("bad name", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation changed on invalid input")
	}
}

func TestValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; first op must not be applied.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "BAD", ""}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	must := func(b Batch) {
		t.Helper()
		if _, e := g.Apply(b); e != nil {
			t.Fatal(e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("failed batch leaked nodes")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, e := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
	// Edge capacity exceeded only at batch end.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = g.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	for i := 1; i < len(s.Edges); i++ {
		p, c := s.Edges[i-1], s.Edges[i]
		if p.From > c.From || (p.From == c.From && p.To >= c.To) {
			t.Fatalf("edges not sorted: %v", s.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From == "zz" {
		t.Fatal("snapshot shares memory with internal state")
	}
}

func TestDeleteNodeCascadeAndReachable(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{From: "a", To: "c"}) {
		t.Fatalf("cascade failed: %+v", s.Edges)
	}
	ok, e := g.Reachable("a", "c")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	ok, e = g.Reachable("c", "a")
	if e != nil || ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "n00"}}})
			_, _ = g.Reachable("n00", n)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, "n00"}}})
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 {
		t.Fatal(len(s.Nodes))
	}
	for _, e := range s.Edges {
		if e.To != "n00" {
			t.Fatalf("unexpected edge %+v", e)
		}
	}
}
