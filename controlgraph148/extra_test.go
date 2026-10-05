package controlgraph148

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
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{Kind: AddNode, From: ""}}},
		{Ops: []Op{{Kind: AddNode, From: "Upper"}}},
		{Ops: []Op{{Kind: AddNode, From: "has space"}}},
		{Ops: []Op{{Kind: AddNode, From: "toolongname"}}},
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a"}}},
		{Ops: []Op{{Kind: DeleteEdge, From: "a", To: ""}}},
	}
	for _, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, err)
		}
	}
	if got := g.Snapshot().Generation; got != 0 {
		t.Fatalf("invalid batches changed generation to %d", got)
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

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, err := g.Apply(b); !errors.Is(err, want) {
			t.Fatalf("want %v got %v", want, err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
}

func TestCapacityRollback(t *testing.T) {
	g, err := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Intermediate state exceeds node capacity but final state fits.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	// Final edge capacity exceeded: whole batch rolls back.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "c", ""}, {AddEdge, "b", "a"}, {AddEdge, "a", "c"}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 1 || s.Nodes[0] != "b" || len(s.Edges) != 0 || s.Generation != 1 {
		t.Fatal(s)
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
	g, _ := New(Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "a"}, {AddEdge, "a", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	if !reflect.DeepEqual(s.Edges, []Edge{{"a", "c"}, {"b", "a"}}) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(4, []string{"w"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("n-%d", i)
			_, _ = c.Apply("w", Batch{Ops: []Op{{AddNode, name, ""}}})
			_, _ = c.Apply("w", Batch{Ops: []Op{{DeleteNode, name, ""}}})
			_, _ = g.Reachable(name, name)
			_ = g.Snapshot()
			_ = c.Decisions()
			if i%2 == 0 {
				_ = p.ReplaceActors([]string{"w", "x"})
			}
		}()
	}
	wg.Wait()
	if got := len(c.Decisions()); got != 32 {
		t.Fatal(got)
	}
	for i, d := range c.Decisions() {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", d)
		}
	}
}

func TestPolicyLimits(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("b", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 2); err != nil {
		t.Fatal(err)
	}
	if err := p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("a", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
