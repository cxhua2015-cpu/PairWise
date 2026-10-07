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
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts=%+v err=%v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname"}
	for _, n := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("extra field accepted")
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", ""}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unknown kind accepted")
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("self edge accepted")
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-n_1", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("failed batch mutated state")
	}
	// Edge capacity: adding two edges then exceeding rolls back both.
	_, err = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}, {AddEdge, "a", "a"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("rolled-back edges leaked")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "bad name"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone lost logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 1 || len(c.Snapshot().Edges) != 0 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewEmptyBatch(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, s, z, err := g.Preview(Batch{})
	if err != nil || r.Generation != 1 || s.Generation != 1 || z.Generation != 1 || z.Nodes != 1 {
		t.Fatalf("r=%+v s=%+v z=%+v err=%v", r, s, z, err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 12})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, "n-0", n}}})
			_, _ = g.Reachable("n-0", n)
			_ = g.Snapshot()
			_ = g.Stats()
			_, _, _, _ = g.Preview(Batch{Ops: []Op{{DeleteNode, n, ""}}})
			_, _ = g.Clone()
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != 16 {
		t.Fatal(len(s.Nodes))
	}
	for i := 1; i < len(s.Nodes); i++ {
		if s.Nodes[i-1] >= s.Nodes[i] {
			t.Fatal("nodes not sorted")
		}
	}
	for i := 1; i < len(s.Edges); i++ {
		a, b := s.Edges[i-1], s.Edges[i]
		if a.From > b.From || (a.From == b.From && a.To >= b.To) {
			t.Fatal("edges not sorted")
		}
	}
}
