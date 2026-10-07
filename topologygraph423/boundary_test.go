package topologygraph423

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "toolongname", ""},
		{AddNode, "a", "extra"},
		{DeleteNode, "a", "extra"},
		{AddEdge, "a", ""},
		{AddEdge, "", "b"},
		{AddEdge, "a", "a"},
		{DeleteEdge, "a", "a"},
	}
	for _, op := range cases {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatalf("failed batches mutated state: %+v", got)
	}
}

func TestSemanticErrorsAndRollback(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, err := g.Apply(b); !errors.Is(err, want) {
			t.Fatalf("got %v want %v", err, want)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	// Rollback: first op valid, second fails; nothing may be committed.
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {DeleteNode, "zz", ""}}}, ErrNotFound)
	if got := g.Snapshot(); !reflect.DeepEqual(got.Nodes, []string{"a"}) || got.Generation != 1 {
		t.Fatalf("rollback violated: %+v", got)
	}
	// Capacity checked only at batch end.
	g2, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 4})
	must2 := func(b Batch, want error) {
		if _, err := g2.Apply(b); !errors.Is(err, want) {
			t.Fatalf("got %v want %v", err, want)
		}
	}
	must2(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}, ErrCapacity)
	if n := len(g2.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity rollback violated: %d nodes", n)
	}
	// Delete then add within one batch fits final capacity.
	must2(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}, nil)
	must2(Batch{Ops: []Op{{DeleteNode, "a", ""}, {AddNode, "c", ""}, {AddNode, "d", ""}}}, ErrCapacity)
	if got := g2.Snapshot(); !reflect.DeepEqual(got.Nodes, []string{"a", "b"}) {
		t.Fatalf("capacity rollback violated: %+v", got)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("empty batch changed generation: %+v", r)
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("bad name", "b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if ok, err := g.Reachable("a", "a"); !ok || err != nil {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b"}) || !reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}}) {
		t.Fatalf("unstable sort: %+v", s)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	if got := g.Snapshot(); got.Nodes[0] != "a" || got.Edges[0].From != "a" {
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
		t.Fatal("clone lost logical clock")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if g.Stats().Generation != 1 || len(g.Snapshot().Edges) != 1 {
		t.Fatal("clone mutation leaked into original")
	}
}

func TestPreviewEmptyBatch(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	r, snap, stats, err := g.Preview(Batch{})
	if err != nil || r.Generation != 1 || snap.Generation != 1 || stats.Generation != 1 {
		t.Fatalf("%+v %+v %+v %v", r, snap, stats, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "other"}}})
				_ = g.Snapshot()
				_ = g.Stats()
				_, _, _, _ = g.Preview(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
			}
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 16 {
		t.Fatalf("got %d nodes", got)
	}
	if g.Stats().Generation == 0 {
		t.Fatal("generation did not advance")
	}
}
