package controlgraph168

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
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-0_", "12345678"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
}

func TestUnknownKindAndExtraField(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(0), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind(99), "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	mk := func(ops ...Op) error { _, e := g.Apply(Batch{Ops: ops}); return e }
	if e := mk(Op{AddNode, "a", ""}); e != nil {
		t.Fatal(e)
	}
	if e := mk(Op{AddNode, "a", ""}, Op{AddNode, "a", ""}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteNode, "ghost", ""}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := mk(Op{AddEdge, "a", "ghost"}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteEdge, "a", "b"}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := mk(Op{AddNode, "b", ""}, Op{AddEdge, "a", "b"}, Op{AddEdge, "a", "b"}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("self-loop edge leaked")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) && !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestEdgeCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 1, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation must not change on failed batch")
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{})
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

func TestDeleteNodeCascadeAndSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
		{DeleteNode, "b", ""},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	want := Snapshot{Generation: 1, Nodes: []string{"a", "c"}, Edges: []Edge{{"a", "c"}}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("%+v", s)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if !reflect.DeepEqual(g.Snapshot(), want) {
		t.Fatal("snapshot shares internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("BAD", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
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
	g, _ := New(Options{MaxNodes: 1000, MaxEdges: 1000, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 20; j++ {
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

func TestConcurrentEdgesDAG(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 512, MaxNameBytes: 8})
	names := []string{"a", "b", "c", "d", "e", "f"}
	var ops []Op
	for _, n := range names {
		ops = append(ops, Op{AddNode, n, ""})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			i, j := i, j
			w.Add(1)
			go func() {
				defer w.Done()
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, names[i], names[j]}}})
			}()
		}
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Edges) != 15 {
		t.Fatal(len(s.Edges))
	}
	for i := 1; i < len(s.Edges); i++ {
		if s.Edges[i-1].From > s.Edges[i].From ||
			(s.Edges[i-1].From == s.Edges[i].From && s.Edges[i-1].To >= s.Edges[i].To) {
			t.Fatal("edges not sorted")
		}
	}
}
