package topologygraph433

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestOptionsAndNameValidation(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
	g := graph(t)
	bad := []Op{
		{Kind: 0, From: "a"}, {Kind: 99, From: "a"},
		{AddNode, "", ""}, {AddNode, "A", ""}, {AddNode, "a b", ""},
		{AddNode, "toolongname", ""}, {AddNode, "a", "extra"},
		{DeleteNode, "a", "extra"},
		{AddEdge, "a", ""}, {AddEdge, "a", "a"}, {AddEdge, "a!", "b"},
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Stats().Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("generation=%d", r.Generation)
	}
}

func TestBatchRollbackAtomicity(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if err != ErrExists {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) || g.Stats().Generation != before.Generation {
		t.Fatal("failed batch mutated state")
	}
	_, err = g.Apply(Batch{Ops: []Op{{DeleteNode, "missing", ""}}})
	if err != ErrNotFound {
		t.Fatal(err)
	}
	_, err = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}})
	if err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("capacity failure must roll back")
	}
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteNodeRemovesIncidentEdges(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"}, {DeleteNode, "b", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("bad name", "x"); err != ErrInvalidInput {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if ok, err := g.Reachable("a", "a"); !ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}, {"a", "c"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone must preserve generation")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 {
		t.Fatal("clone write leaked into original")
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "z", ""}}})
	if c.Stats().Nodes != 1 {
		t.Fatal("original write leaked into clone")
	}
}

func TestPreviewCapacityErrorParity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: 8})
	b := Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}
	if _, _, _, err := g.Preview(b); err != ErrCapacity {
		t.Fatal(err)
	}
	if _, err := g.Apply(b); err != ErrCapacity {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 0 {
		t.Fatal("preview/apply leaked")
	}
	if _, _, _, err := g.Preview(Batch{Ops: []Op{{Kind: 0}}}); err != ErrInvalidInput {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _, _, _ = g.Preview(Batch{Ops: []Op{{AddNode, n + "x", ""}}})
			_, _ = g.Clone()
			_ = g.Stats()
			_ = g.Snapshot()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, n + "x"}}})
		}()
	}
	wg.Wait()
	if got := g.Stats().Nodes; got != 16 {
		t.Fatal(got)
	}
	if g.Stats().Generation != 16 {
		t.Fatal(g.Stats().Generation)
	}
}

func TestConcurrentEdgeWriters(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 16})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "src", ""}, {AddNode, "dst", ""}}})
	var wg sync.WaitGroup
	wins := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := g.Apply(Batch{Ops: []Op{{AddEdge, "src", "dst"}}})
			wins <- err
		}()
	}
	wg.Wait()
	close(wins)
	var ok, dup int
	for err := range wins {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrExists) {
			dup++
		}
	}
	if ok != 1 || dup != 7 {
		t.Fatalf("ok=%d dup=%d", ok, dup)
	}
}
