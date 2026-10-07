package topologygraph403

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

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{DeleteNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range bad {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: ValidateBatch = %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: Apply = %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r.Generation)
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	before := g.Snapshot()
	// Last op fails: nothing may be committed.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "a", "b"}, {DeleteNode, "zzz", ""}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 2 || len(got.Edges) != 0 {
		t.Fatalf("rollback failed: %+v", got)
	}
	// Capacity exceeded at end of batch: full rollback.
	_, err = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("capacity rollback failed")
	}
}

func TestExistsAndNotFound(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	_, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{From: "a", To: "c"}) {
		t.Fatalf("%+v", s.Edges)
	}
}

func TestSnapshotSortedAndDetached(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "a", "b"}, {AddEdge, "a", "c"}, {AddEdge, "b", "c"},
	}})
	s := g.Snapshot()
	wantN := []string{"a", "b", "c"}
	for i, n := range wantN {
		if s.Nodes[i] != n {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[1] != (Edge{"a", "c"}) || s.Edges[2] != (Edge{"b", "c"}) {
		t.Fatalf("edges not sorted: %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestStatsAndCloneConsistency(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	st := g.Stats()
	if st.Generation != 1 || st.Nodes != 2 || st.Edges != 1 {
		t.Fatalf("%+v", st)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats() != st {
		t.Fatal("clone lost logical clock")
	}
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}})
	if g.Stats().Nodes != 2 || c.Stats().Nodes != 1 {
		t.Fatal("clone shares ownership with original")
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
			n := fmt.Sprintf("n%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "n0"}}})
			_ = g.Snapshot()
			_ = g.Stats()
			_, _ = g.Reachable("n0", n)
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
	if g.Stats().Generation != 32 {
		t.Fatal(g.Stats().Generation)
	}
}

func TestConcurrentCloneAndApply(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 16})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		w.Add(1)
		go func() {
			defer w.Done()
			c, err := g.Clone()
			if err != nil {
				t.Error(err)
				return
			}
			if c.Stats().Nodes != 2 {
				t.Error("inconsistent clone")
			}
		}()
	}
	w.Wait()
}
