package controlgraph133

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptionsAndInput(t *testing.T) {
	if _, err := New(Options{MaxNodes: 0, MaxEdges: 1, MaxNameBytes: 1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := New(Options{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: 0}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{Kind: AddNode, From: ""}}},
		{Ops: []Op{{Kind: AddNode, From: "Upper"}}},
		{Ops: []Op{{Kind: AddNode, From: "toolongname"}}},
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a", To: ""}}},
	}
	for _, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, err)
		}
	}
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatal("invalid batch mutated state")
	}
}

func TestSemanticsAndRollback(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// Capacity exceeded only at batch end: whole batch rolls back.
	big := Batch{}
	for i := 0; i < 6; i++ {
		big.Ops = append(big.Ops, Op{Kind: AddNode, From: fmt.Sprintf("n%d", i)})
	}
	if _, err := g.Apply(big); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Nodes) != 0 || s.Generation != 0 {
		t.Fatal("failed batch was not rolled back")
	}
	// Self-loop is a cycle.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	// Empty batch succeeds without bumping generation.
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(err, r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if s.Nodes[0] != "a" || s.Nodes[1] != "b" || s.Edges[0] != (Edge{From: "a", To: "b"}) {
		t.Fatal("snapshot not sorted", s)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(4, []string{"writer"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("node-%d", i)
			_, _ = c.Apply("writer", Batch{Ops: []Op{{Kind: AddNode, From: n}}})
			_, _ = c.Apply("intruder", Batch{Ops: []Op{{Kind: AddNode, From: n}}})
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
			_ = c.Decisions()
			if i%3 == 0 {
				_ = p.ReplaceActors([]string{"writer", fmt.Sprintf("a%d", i)})
			}
		}()
	}
	wg.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
	ds := c.Decisions()
	if len(ds) != 64 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("audit sequence not contiguous", ds)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}
