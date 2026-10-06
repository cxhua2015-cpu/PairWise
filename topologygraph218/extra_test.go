package topologygraph218

import (
	"errors"
	"fmt"
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
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(0), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "extra"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
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
	must(Batch{Ops: []Op{{DeleteNode, "b", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}, ErrExists)
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if r.Generation != 2 || g.Snapshot().Generation != 2 {
		t.Fatal(r)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("A", "b"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if s.Nodes[0] != "a" || s.Nodes[1] != "b" || s.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal(s)
	}
	s.Nodes[0] = "zzz"
	s.Edges[0] = Edge{"z", "z"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal(s2)
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
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
}
