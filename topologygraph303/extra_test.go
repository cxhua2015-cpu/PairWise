package topologygraph303

import (
	"errors"
	"fmt"
	"reflect"
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
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{Kind: AddNode, From: ""}}},
		{Ops: []Op{{Kind: AddNode, From: "A"}}},
		{Ops: []Op{{Kind: AddNode, From: "a b"}}},
		{Ops: []Op{{Kind: AddNode, From: "toolongname"}}},
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: DeleteNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a"}}},
		{Ops: []Op{{Kind: DeleteEdge, From: "a", To: ""}}},
	}
	for i, b := range bad {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	// Structural validation happens before state reads: unknown kind must
	// win even if an earlier op would fail against state.
	_, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "missing", ""}, {Kind: 42}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatal(s)
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
	r, e = g.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestRollbackAtomicity(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Fails midway: second op duplicates, cycle, capacity — nothing may stick.
	for _, b := range []Batch{
		{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}}},
		{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "a"}, {AddEdge, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", "c"}}},
		{Ops: []Op{{DeleteEdge, "a", "c"}}},
		{Ops: []Op{{DeleteNode, "zz", ""}}},
	} {
		if _, e := g.Apply(b); e == nil {
			t.Fatalf("expected error for %+v", b)
		}
		if got := g.Snapshot(); !reflect.DeepEqual(before, got) {
			t.Fatalf("state changed: %+v -> %+v", before, got)
		}
	}
	// Capacity exceeded at batch end rolls back too.
	full, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	_, _ = full.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	_, e = full.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := full.Snapshot().Nodes; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatal(got)
	}
}

func TestSelfLoopAndDuplicate(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
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
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating the returned slices must not affect the graph.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("BAD!", "b"); !errors.Is(e, ErrInvalidInput) {
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
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "hub"}}})
				_, _ = g.Reachable(n, "hub")
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}})
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 128 || len(s.Edges) > 256 {
		t.Fatal("capacity violated")
	}
}
