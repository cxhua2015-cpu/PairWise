package trustgraph

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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "toolongname", "é"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok_nm-1", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidationFirst(t *testing.T) {
	g := graph(t)
	// Unknown kind and extra fields must be rejected even if other ops are fine.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "b", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Fails midway: duplicate node add.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// Fails at capacity check at end of batch.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batches")
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	if g.Snapshot().Generation != 0 {
		t.Fatal()
	}
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) || g.Snapshot().Generation != 1 {
		t.Fatal("failed batch must not bump generation")
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatal(s)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
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
	wantNodes := []string{"a", "b", "c"}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if !reflect.DeepEqual(s.Nodes, wantNodes) || !reflect.DeepEqual(s.Edges, wantEdges) {
		t.Fatal(s)
	}
	// Mutating returned slices must not affect the graph.
	s.Nodes[0] = "zzz"
	s.Edges[0].From = "zzz"
	s2 := g.Snapshot()
	if !reflect.DeepEqual(s2.Nodes, wantNodes) || !reflect.DeepEqual(s2.Edges, wantEdges) {
		t.Fatal("snapshot shares memory with graph")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Reachable("a", "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "bad name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 10; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "hub", n}}})
				_, _ = g.Reachable("hub", n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "hub", n}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 33 {
		t.Fatal(len(s.Nodes))
	}
}
