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
	if _, err := New(Options{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	g := graph(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "Upper", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "waytoolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
	}
	for i, b := range bad {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if got := g.Snapshot().Generation; got != 0 {
		t.Fatal(got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Duplicate add mid-batch must roll back the earlier adds.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 1 {
		t.Fatal(got)
	}
	// Capacity overflow at batch end rolls back everything.
	g2, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	_, err = g2.Apply(Batch{Ops: []Op{{AddNode, "x", ""}, {AddNode, "y", ""}, {AddNode, "z", ""}}})
	if !errors.Is(err, ErrCapacity) || len(g2.Snapshot().Nodes) != 0 {
		t.Fatal(err, g2.Snapshot())
	}
	// DeleteEdge of a missing edge and DeleteNode of a missing node.
	if _, err = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"}, {AddEdge, "c", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	want := []string{"a", "b", "c"}
	for i, n := range want {
		if s.Nodes[i] != n {
			t.Fatal(s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[1] != (Edge{"c", "a"}) || s.Edges[2] != (Edge{"c", "b"}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
	if ok, err := g.Reachable("c", "b"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := g.Reachable("c", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(4, []string{"alice", "bob"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%2 == 1 {
				actor = "bob"
			}
			name := fmt.Sprintf("n-%d", i)
			_, _ = c.Apply(actor, Batch{Ops: []Op{{AddNode, name, ""}}})
			_, _ = g.Reachable(name, name)
			_ = g.Snapshot()
			_ = c.Decisions()
			if i%3 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob", "carol"})
			}
		}()
	}
	wg.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
	ds := c.Decisions()
	if len(ds) != 32 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", d.Sequence)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{"Bad Name"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, _ := NewPolicy(2, []string{"ok"})
	if err := p.Authorize("ok", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("ok", 2); err != nil {
		t.Fatal(err)
	}
	if err := p.ReplaceActors([]string{"nope!"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Failed replacement leaves the previous list intact.
	if err := p.Authorize("ok", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCoordinator(nil, p); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}
