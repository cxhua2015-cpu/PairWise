package topologygraph413

import (
	"errors"
	"fmt"
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
	bad := []Op{
		{Kind: 0, From: "a"},
		{Kind: 99, From: "a"},
		{Kind: AddNode, From: ""},
		{Kind: AddNode, From: "A"},
		{Kind: AddNode, From: "a b"},
		{Kind: AddNode, From: "toolongname"},
		{Kind: AddNode, From: "a", To: "b"},
		{Kind: AddEdge, From: "a", To: ""},
		{Kind: AddEdge, From: "a", To: "a"},
		{Kind: DeleteEdge, From: "", To: "b"},
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatalf("failed validation mutated state: %+v", s)
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if after := g.Snapshot(); after.Generation != before.Generation || len(after.Nodes) != 1 {
		t.Fatalf("rollback failed: %+v", after)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity failure must roll back, got %d nodes", n)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	_, err = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 2 || len(s.Edges) != 1 {
		t.Fatalf("rollback failed: %+v", s)
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatalf("generation must advance once per batch, got %d", r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zzz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if g.Stats().Generation != 2 {
		t.Fatal("failed batch must not advance generation")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("self reachability: %v %v", ok, err)
	}
}

func TestCloneIsolation(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != g.Stats().Generation {
		t.Fatal("clone must preserve generation")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Nodes) != 2 || len(s.Edges) != 1 {
		t.Fatalf("clone mutated original: %+v", s)
	}
	if s := c.Snapshot(); len(s.Nodes) != 1 || len(s.Edges) != 0 {
		t.Fatalf("original mutated clone: %+v", s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "x"}}})
				_ = g.Stats()
				_ = g.Snapshot()
				_, _ = g.Reachable(n, n)
				_, _ = g.Clone()
			}
			_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
		}()
	}
	wg.Wait()
	if s := g.Snapshot(); len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Fatalf("leaked state: %+v", s)
	}
}
