package controlgraph143

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

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatalf("invalid batch mutated state: %+v", got)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch) {
		t.Helper()
		if _, err := g.Apply(b); err != nil {
			t.Fatal(err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("rolled back batch left edges")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatalf("capacity failure not rolled back: %+v", got)
	}
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != 1 || len(got.Edges) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatal(r, err)
	}
	if _, err = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{Ops: nil})
	if err != nil || r.Generation != 1 {
		t.Fatal(r, err)
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
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
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
		t.Fatal(ok, err)
	}
	if _, err = g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPolicyValidation(t *testing.T) {
	if _, err := NewPolicy(0, []string{"a"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	if _, err := NewPolicy(1, []string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	p, err := NewPolicy(2, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 3); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.Authorize("b", 1); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 2); err != nil {
		t.Fatal(err)
	}
	if err = p.ReplaceActors([]string{""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if err = p.Authorize("a", 1); err != nil {
		t.Fatal("failed replace must keep old actors", err)
	}
}

func TestCoordinatorNilAndEngineFailure(t *testing.T) {
	if _, err := NewCoordinator(nil, &Policy{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	g := graph(t)
	p, _ := NewPolicy(4, []string{"a"})
	c, err := NewCoordinator(g, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply("a", Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	d := c.Decisions()
	if len(d) != 1 || d[0].Sequence != 1 || d[0].Committed || d[0].Error == "" {
		t.Fatalf("%+v", d)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	p, _ := NewPolicy(3, []string{"w"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("n-%d", i)
			for j := 0; j < 20; j++ {
				_, _ = c.Apply("w", Batch{Ops: []Op{{AddNode, name, ""}}})
				_, _ = c.Apply("w", Batch{Ops: []Op{{DeleteNode, name, ""}}})
				_, _ = g.Reachable(name, name)
				_ = g.Snapshot()
				_ = c.Decisions()
			}
		}()
	}
	wg.Wait()
	seqs := map[uint64]bool{}
	for _, d := range c.Decisions() {
		if seqs[d.Sequence] {
			t.Fatalf("duplicate sequence %d", d.Sequence)
		}
		seqs[d.Sequence] = true
	}
	if len(seqs) != 16*20*2 {
		t.Fatal(len(seqs))
	}
	for s := uint64(1); s <= uint64(len(seqs)); s++ {
		if !seqs[s] {
			t.Fatalf("gap at sequence %d", s)
		}
	}
}

func TestConcurrentPolicyReplace(t *testing.T) {
	g := graph(t)
	p, _ := NewPolicy(1, []string{"a"})
	c, _ := NewCoordinator(g, p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := fmt.Sprintf("actor-%d", i%2)
			for j := 0; j < 50; j++ {
				_ = p.ReplaceActors([]string{actor})
				_, _ = c.Apply(actor, Batch{Ops: []Op{}})
				_ = c.Decisions()
			}
		}()
	}
	wg.Wait()
}
