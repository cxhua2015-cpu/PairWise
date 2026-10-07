package topologygraph343

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
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for _, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatal(got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r)
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	before := g.Snapshot()
	// Second op fails; first op must not commit.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
	// Capacity failure rolls back the whole batch.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after capacity failure")
	}
}

func TestDeleteNodeCascadeAndNotFound(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "a", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("edges not cascaded")
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "a"}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}, {"c", "a"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zzz"
	s.Edges[0].From = "zzz"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot shares memory with graph")
	}
}

func TestReachableNotFound(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
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
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, n}}}) // always ErrCycle
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}, {AddNode, n, ""}}})
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
}
