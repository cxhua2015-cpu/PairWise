package topologygraph208

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
			t.Fatalf("opts %+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b", "Abc"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok-name1", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(0), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(99), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "extra"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestValidationBeforeState(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	gen := g.Snapshot().Generation
	// Second op is structurally invalid: whole batch rejected before touching state,
	// so the duplicate AddNode must not surface ErrExists.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(0), "x", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != gen {
		t.Fatal("generation changed on failed batch")
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestDuplicateEdge(t *testing.T) {
	g := graph(t)
	ops := []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	ops := []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
		{DeleteNode, "b", ""},
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatal(s.Edges)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	// Adds a node then removes one: final count fits, must succeed.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {DeleteNode, "d", ""}}}); e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Exceeds node capacity at batch end: full rollback.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {AddNode, "e", ""}, {DeleteNode, "d", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// Exceeds edge capacity at batch end: full rollback.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after capacity failures")
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	if g.Snapshot().Generation != 0 {
		t.Fatal()
	}
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
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
	s.Nodes[0] = "mutated"
	s.Edges[0] = Edge{"x", "y"}
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot shares memory with internal state")
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
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
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
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "hub"}}})
				_, _ = g.Reachable(n, "hub")
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, "hub"}}})
			}
		}()
	}
	w.Add(1)
	go func() {
		defer w.Done()
		for j := 0; j < 50; j++ {
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "hub", ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "hub", ""}}})
		}
	}()
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 128 || len(s.Edges) > 256 {
		t.Fatal("capacity violated")
	}
}
