package controlgraph093

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

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind(0), "a", ""}, {Kind(99), "a", ""},
		{AddNode, "", ""}, {AddNode, "A", ""}, {AddNode, "a b", ""}, {AddNode, "a", "x"},
		{AddNode, "toolongname", ""},
		{DeleteNode, "a", "x"},
		{AddEdge, "a", ""}, {AddEdge, "", "b"}, {AddEdge, "Bad", "b"},
		{DeleteEdge, "a", "UP"},
	}
	for _, op := range cases {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("invalid batches must not change state")
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
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "a"}}}, ErrCycle)
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Last op fails: whole batch must roll back.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "a", "b"}, {DeleteNode, "zz", ""}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := g.Snapshot()
	if after.Generation != before.Generation || len(after.Nodes) != 2 || len(after.Edges) != 0 {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("failed batch bumped generation")
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	if r, _ := g.Apply(Batch{}); r.Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
	r, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal()
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

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i := range wantN {
		if s.Nodes[i] != wantN[i] {
			t.Fatal(s.Nodes)
		}
	}
	wantE := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	for i := range wantE {
		if s.Edges[i] != wantE[i] {
			t.Fatal(s.Edges)
		}
	}
	// Mutating the returned snapshot must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot shares state")
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
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal(g.Snapshot().Nodes)
	}
}
