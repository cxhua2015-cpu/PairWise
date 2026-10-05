package failovergraph

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
			t.Fatalf("opts=%+v err=%v", o, e)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: Kind(99), From: "a"},
		{Kind: AddNode, From: ""},
		{Kind: AddNode, From: "A"},
		{Kind: AddNode, From: "a b"},
		{Kind: AddNode, From: "a", To: "b"},
		{Kind: AddNode, From: "toolongname"},
		{Kind: AddEdge, From: "a", To: ""},
		{Kind: DeleteEdge, From: "", To: "b"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("op=%+v err=%v", op, e)
		}
	}
	// Structural validation happens before state reads: unknown kind must win.
	_, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "dup"}, {Kind: 0, From: "x"}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("generation changed on failed batch")
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// Last op fails -> whole batch rolls back.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteEdge, "a", "b"}, {AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Capacity exceeded at batch end -> rollback.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
}

func TestSelfLoopAndNotFound(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "ghost"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "ghost"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("bad name", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating the returned slices must not affect internal state.
	s.Nodes[0] = "zzz"
	s.Edges[0] = Edge{"x", "y"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot shares internal state")
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
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 0 {
		t.Fatal(got)
	}
}

func TestConcurrentDAGInvariant(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 256, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 100; j++ {
				if i%2 == 0 {
					_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}}})
				} else {
					_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}, {AddEdge, "b", "a"}}})
				}
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	// Whatever the interleaving, the graph must remain acyclic.
	if len(s.Edges) > 1 {
		t.Fatalf("cycle formed: %v", s.Edges)
	}
}
