package topologygraph418

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("%+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
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
		{Ops: []Op{{AddEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); err != ErrInvalidInput {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	// Validation is side-effect free: state untouched, generation unchanged.
	if s := g.Snapshot(); s.Generation != 0 || len(s.Nodes) != 0 {
		t.Fatalf("validation mutated state: %+v", s)
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_x", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatalf("generation moved on empty batch: %d", r.Generation)
	}
}

func TestBatchRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 4})
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 || got.Generation != before.Generation {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestExistsNotFoundSemantics(t *testing.T) {
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
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDeleteNodeCascadeAndCycleRollback(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Cyclic op mid-batch must roll back the whole batch.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {AddEdge, "c", "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if s := g.Snapshot(); len(s.Nodes) != 3 || len(s.Edges) != 2 {
		t.Fatalf("cycle batch leaked: %+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 {
		t.Fatalf("cascade failed: %+v", s)
	}
	if ok, err := g.Reachable("a", "c"); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestSnapshotIsolationAndOrdering(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s1 := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s1.Nodes[i] != n {
			t.Fatalf("nodes not sorted: %v", s1.Nodes)
		}
	}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i, e := range wantEdges {
		if s1.Edges[i] != e {
			t.Fatalf("edges not sorted: %v", s1.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s1.Nodes[0] = "zz"
	s1.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != g.Stats() {
		t.Fatal("clone lost logical clock")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "z", ""}}})
	gs, cs := g.Stats(), c.Stats()
	if gs.Nodes != 3 || gs.Edges != 1 || cs.Nodes != 1 || cs.Edges != 0 {
		t.Fatalf("clone aliases original: g=%+v c=%+v", gs, cs)
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
			n := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.Stats()
			_ = g.Snapshot()
			_, _ = g.Reachable(n, n)
			if i%8 == 0 {
				_, _ = g.Clone()
			}
		}()
	}
	w.Wait()
	for i := 0; i < 32; i += 2 {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, fmt.Sprintf("n%02d", i), fmt.Sprintf("n%02d", i+1)}}})
		}()
	}
	w.Wait()
	st := g.Stats()
	if st.Nodes != 32 || st.Edges != 16 {
		t.Fatalf("%+v", st)
	}
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
}
