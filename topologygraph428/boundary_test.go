package topologygraph428

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "Upper", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "", "b"}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); err != ErrInvalidInput {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Nodes != 0 || s.Generation != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
}

func TestSemanticErrorsAndRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Batch that fails mid-way must roll back fully.
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatalf("rollback: %v %+v", err, g.Snapshot())
	}
	if g.Stats().Generation != 1 {
		t.Fatalf("failed batch bumped generation: %+v", g.Stats())
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "c"}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("capacity failure mutated state")
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatalf("generation: %+v", r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatalf("non-empty batch bumps once: %+v", r)
	}
	if g.Snapshot().Generation != 2 || g.Stats().Generation != 2 {
		t.Fatal("generation mismatch")
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatalf("nodes not sorted: %v", s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatalf("edges not sorted: %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0] = Edge{"x", "y"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("zz", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("self reachable: %v %v", ok, err)
	}
}

func TestCloneIsolationAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone must preserve logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Nodes != 2 || g.Stats().Edges != 1 || g.Stats().Generation != 1 {
		t.Fatalf("clone write leaked: %+v", g.Stats())
	}
	if c.Stats().Generation != 2 {
		t.Fatalf("clone clock: %+v", c.Stats())
	}
}

func TestPreviewCycleErrorParity(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	b := Batch{Ops: []Op{{AddEdge, "b", "a"}}}
	_, _, _, perr := g.Preview(b)
	_, aerr := g.Apply(b)
	if !errors.Is(perr, ErrCycle) || !errors.Is(aerr, ErrCycle) {
		t.Fatalf("parity: preview=%v apply=%v", perr, aerr)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _, _, _ = g.Preview(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			_, _ = g.Clone()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
			_, _ = g.Reachable(n, n)
			_ = g.Stats()
			_ = g.Snapshot()
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatalf("nodes=%d", got)
	}
	if g.Stats().Generation != 32 {
		t.Fatalf("generation=%d", g.Stats().Generation)
	}
}

func TestConcurrentApplySerialize(t *testing.T) {
	g, _ := New(Options{MaxNodes: 1000, MaxEdges: 1000, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			// All goroutines add the same node; exactly one wins.
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "shared", ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, fmt.Sprintf("u%d", i), ""}}})
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 51 {
		t.Fatalf("nodes=%d", got)
	}
}
