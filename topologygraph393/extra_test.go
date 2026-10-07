package topologygraph393

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
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
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
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation changed on invalid input")
	}
}

func TestValidNames(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a-b_c1", ""}}}); e != nil {
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
	if e != nil || r.Generation != 1 {
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
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal(s)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) && !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("rolled back edges leaked")
	}
}

func TestDeleteNodeCascadeAndMidBatch(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
		{DeleteNode, "b", ""},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || !reflect.DeepEqual(s.Nodes, []string{"a", "c"}) {
		t.Fatal(s)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
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
	s.Nodes[0] = "zzz"
	s.Edges[0] = Edge{"z", "z"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot not isolated")
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
			n := fmt.Sprintf("n-%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "n-00", n}}})
			_, _ = g.Reachable("n-00", n)
			_ = g.Snapshot()
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 32 {
		t.Fatal(len(s.Nodes))
	}
}
