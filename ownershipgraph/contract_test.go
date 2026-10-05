package ownershipgraph

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func graph(t *testing.T) *Graph {
	t.Helper()
	g, e := New(Options{MaxNodes: 5, MaxEdges: 6, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func TestReachAndCycle(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "c"}}})
	if e != nil {
		t.Fatal(e)
	}
	ok, _ := g.Reachable("a", "c")
	if !ok {
		t.Fatal()
	}
	b := g.Snapshot()
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}})
	if !errors.Is(e, ErrCycle) || !reflect.DeepEqual(b, g.Snapshot()) {
		t.Fatal(e)
	}
}
func TestSequentialDelete(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "b", ""}, {AddNode, "b", ""}}})
	if e != nil || len(g.Snapshot().Edges) != 0 {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	_, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}, {AddNode, "b", ""}}})
	if e != nil || g.Snapshot().Nodes[0] != "b" {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := string(rune('a' + i))
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.Snapshot()
		}()
	}
	w.Wait()
	if len(g.Snapshot().Nodes) != 20 {
		t.Fatal(len(g.Snapshot().Nodes))
	}
}
