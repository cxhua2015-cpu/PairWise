package controlgraph173

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
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range bad {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%d: %v", i, e)
		}
	}
	if _, e := g.Reachable("BAD", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal(s)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch) {
		t.Helper()
		if _, e := g.Apply(b); e != nil {
			t.Fatal(e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz", }}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
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
		t.Fatal("rollback failed")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("rollback failed")
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Edges); n != 0 {
		t.Fatal(n)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
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
	if r.Generation != 1 {
		t.Fatal(r)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "a"}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"c", "a"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
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
			m := fmt.Sprintf("n%02d", (i+1)%16)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
			_, _ = g.Reachable(n, m)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 16 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}
