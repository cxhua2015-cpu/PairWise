package controlgraph163

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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"}
	for _, n := range bad {
		_, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok-name1", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	g := graph(t)
	// Unknown kind and extra To field must fail even though "a" does not exist.
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(99), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "extra"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// A later invalid op must abort the batch before earlier ops take effect.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(0), "b", ""}}})
	if !errors.Is(e, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{Ops: nil})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "missing"}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != before.Generation {
		t.Fatal("generation changed on failed batch")
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestSelfLoopAndIndirectCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "c"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "b"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	_, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if e != nil || len(g.Snapshot().Edges) != 0 {
		t.Fatal(e)
	}
	// Re-adding b must not resurrect edges.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}}})
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal()
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Swap within capacity succeeds.
	_, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}, {AddNode, "c", ""}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
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
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot shares state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Reachable("a", "bad name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
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
			n := fmt.Sprintf("n%02d", i)
			m := fmt.Sprintf("n%02d", (i+1)%32)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
			_, _ = g.Reachable(n, m)
			s := g.Snapshot()
			if len(s.Nodes) > 128 {
				t.Error("capacity violated")
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 {
		t.Fatal(len(s.Nodes))
	}
}
