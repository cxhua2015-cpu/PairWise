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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname"}
	for _, n := range bad {
		if _, err := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "ok_nm-1", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{Kind(0), "a", ""}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind(99), "a", ""}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	// Node ops must not carry an extra To field.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// First op is valid, second is malformed: nothing may be applied.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(0), "b", ""}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(g.Snapshot())
	}
}

func TestEmptyBatchNoGenerationBump(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
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
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Second op fails (duplicate): the whole batch must roll back.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(g.Snapshot())
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	// Transiently exceeds capacity (3 nodes) but ends within limits: allowed.
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}}); err != nil {
		t.Fatal(err)
	}
	// Final state exceeds capacity: rejected and rolled back.
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal(g.Snapshot())
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
		{DeleteNode, "b", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{From: "a", To: "c"}) {
		t.Fatal(s.Edges)
	}
}

func TestDeleteEdgeNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSelfLoopIsCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating the returned snapshot must not affect internal state.
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableMissingNode(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
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

func TestConcurrentMixed(t *testing.T) {
	g, err := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Reachable(n, n)
			_ = g.Snapshot()
		}()
	}
	wg.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
}

func TestPolicyBasics(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{"bad actor"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("alice", 2); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("alice", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.Authorize("mallory", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := p.ReplaceActors(nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Authorize("alice", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestCoordinatorEngineFailureSequence(t *testing.T) {
	core, err := New(Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(4, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCoordinator(core, p)
	if err != nil {
		t.Fatal(err)
	}
	// Engine failure (duplicate node) still consumes an audit sequence.
	if _, err := c.Apply("alice", Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Apply("alice", Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := c.Apply("mallory", Batch{Ops: []Op{{AddNode, "b", ""}}}); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	ds := c.Decisions()
	if len(ds) != 3 {
		t.Fatal(ds)
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal(ds)
		}
	}
	if !ds[0].Committed || ds[1].Committed || ds[2].Committed {
		t.Fatal(ds)
	}
	if ds[1].Error == "" || ds[2].Error == "" {
		t.Fatal(ds)
	}
}

func TestConcurrentCoordinator(t *testing.T) {
	core, err := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(4, []string{"alice", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCoordinator(core, p)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "alice"
			if i%2 == 1 {
				actor = "bob"
			}
			_, _ = c.Apply(actor, Batch{Ops: []Op{{AddNode, fmt.Sprintf("n-%d", i), ""}}})
			_ = c.Decisions()
		}()
	}
	// Concurrent policy replacement must be race-free.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 8; j++ {
			_ = p.ReplaceActors([]string{"alice", "bob"})
		}
	}()
	wg.Wait()
	ds := c.Decisions()
	if len(ds) != 16 {
		t.Fatal(len(ds))
	}
	for i, d := range ds {
		if d.Sequence != uint64(i+1) {
			t.Fatal("non-contiguous sequence", ds)
		}
	}
}
