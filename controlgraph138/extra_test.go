package controlgraph138

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{Kind: AddNode, From: ""}}},
		{Ops: []Op{{Kind: AddNode, From: "A"}}},
		{Ops: []Op{{Kind: AddNode, From: "a b"}}},
		{Ops: []Op{{Kind: AddNode, From: "toolongname"}}},
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a", To: ""}}},
	}
	for _, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, err)
		}
	}
	if got := g.Snapshot().Generation; got != 0 {
		t.Fatalf("generation moved on invalid input: %d", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("empty batch after commit: %v %v", r, err)
	}
}

func TestRollbackOnCapacity(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 {
		t.Fatalf("state leaked after rollback: %+v", got)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
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
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] == "mutated" || again.Edges[0].From == "mutated" {
		t.Fatal("snapshot aliases internal state")
	}
	if again.Nodes[0] != "a" || again.Nodes[1] != "b" {
		t.Fatalf("nodes not sorted: %v", again.Nodes)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(4, []string{"alice", "bob"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%3 == 0 {
				actor = "mallory"
			}
			name := fmt.Sprintf("node-%d", i)
			_, _ = c.Apply(actor, Batch{Ops: []Op{{AddNode, name, ""}}})
			_, _ = g.Reachable(name, name)
			_ = g.Snapshot()
			_ = c.Decisions()
			if i%2 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	wg.Wait()
	decisions := c.Decisions()
	for i, d := range decisions {
		if d.Sequence != uint64(i+1) {
			t.Fatalf("non-contiguous sequence at %d: %+v", i, d)
		}
	}
	s := g.Snapshot()
	if len(s.Nodes) == 0 {
		t.Fatal("expected committed nodes")
	}
}
