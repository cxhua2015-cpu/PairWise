package topologygraph218

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
	cases := []Batch{
		{Ops: []Op{{Kind(0), "a", ""}}},
		{Ops: []Op{{Kind(99), "a", ""}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "a/b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatal("state mutated by invalid batches")
	}
}

func TestValidNameChars(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a-z_0", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r, e = g.Apply(Batch{Ops: []Op{}})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r, e)
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
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("rollback failed")
	}
	// edge capacity rollback
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	before = g.Snapshot()
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}, {AddEdge, "a", "a"}}})
	if !errors.Is(e, ErrCycle) && !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("rollback failed")
	}
}

func TestRollbackRestoresIncidentEdges(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	// delete b (removes edge), then add c -> exceeds MaxNodes at end -> rollback
	_, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatalf("rollback failed: %+v", g.Snapshot())
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	_, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatal(s)
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
	// mutating returned slices must not affect internal state
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot not isolated")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Reachable("a", "zz"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("BAD", "a"); !errors.Is(e, ErrInvalidInput) {
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
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			}
		}()
	}
	w.Wait()
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatal(s)
	}
}

func TestConcurrentEdgeWriters(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			for j := 0; j < 100; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}})
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Edges) > 1 {
		t.Fatal(s)
	}
}
