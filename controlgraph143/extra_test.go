package controlgraph143

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptionsAndInput(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
	g := graph(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{Kind: AddNode, From: ""}}},
		{Ops: []Op{{Kind: AddNode, From: "Upper"}}},
		{Ops: []Op{{Kind: AddNode, From: "a b"}}},
		{Ops: []Op{{Kind: AddNode, From: "toolongname"}}},
		{Ops: []Op{{Kind: AddNode, From: "a", To: "b"}}},
		{Ops: []Op{{Kind: AddEdge, From: "a"}}},
	}
	for _, b := range bad {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("batch %+v: %v", b, err)
		}
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatalf("invalid batch mutated state: %+v", got)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
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
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("rollback failed, %d nodes", n)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestSelfLoopAndGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal("empty batch changed generation")
	}
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal("failed batch bumped generation")
	}
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err = g.Reachable("a", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = g.Reachable("BAD", "a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "b", "a"}, {AddEdge, "a", "c"}}})
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
	s.Nodes[0] = "zzz"
	s.Edges[0].From = "zzz"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("alice", 2); err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("alice", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.Authorize("mallory", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("alice", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.ReplaceActors([]string{"bad actor"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestCoordinatorNilAndEngineFailure(t *testing.T) {
	if _, err := NewCoordinator(nil, &Policy{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	core, _ := New(Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	p, _ := NewPolicy(4, []string{"alice"})
	c, err := NewCoordinator(core, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply("alice", Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	// Engine failure (duplicate node) still gets a sequence number.
	if _, err = c.Apply("alice", Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 2 || d[0].Sequence != 1 || d[1].Sequence != 2 || d[1].Committed || d[1].Error == "" {
		t.Fatal(d)
	}
	if core.Snapshot().Generation != 1 {
		t.Fatal("engine failure mutated state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	core, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(8, []string{"alice", "bob"})
	c, _ := NewCoordinator(core, p)
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
			_, _ = c.Apply(actor, Batch{Ops: []Op{{DeleteNode, name, ""}}})
			_, _ = core.Reachable(name, name)
			_ = core.Snapshot()
			_ = c.Decisions()
			if i%3 == 0 {
				_ = p.ReplaceActors([]string{"alice", "bob"})
			}
		}()
	}
	wg.Wait()
	d := c.Decisions()
	if len(d) != 64 {
		t.Fatal(len(d))
	}
	for i, dec := range d {
		if dec.Sequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %+v", i, dec)
		}
	}
}
