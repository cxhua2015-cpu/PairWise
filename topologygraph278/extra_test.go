package topologygraph278

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestOptionsInvalid(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{AddNode, "", ""},
		{AddNode, "A", ""},
		{AddNode, "a b", ""},
		{AddNode, "a/b", ""},
		{AddNode, "toolongname", ""}, // exceeds MaxNameBytes=8
		{AddNode, "ok", "extra"},     // extra field
		{Kind(0), "a", ""},           // unknown kind
		{Kind(99), "a", ""},
		{AddEdge, "a", "a"}, // self loop
	}
	for _, op := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("op %+v: %v", op, err)
		}
		if _, err := g.Apply(Batch{Ops: []Op{op}}); err != ErrInvalidInput {
			t.Fatalf("apply op %+v: %v", op, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "a-b_c1", ""}}}); err != nil {
		t.Fatal(err)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "zz"); err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestAtomicRollback(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "nope", ""},
	}})
	if err != ErrNotFound {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("rollback leaked state: %+v", s)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
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
		t.Fatalf("multi-op batch must bump generation once: %d", r.Generation)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); err == nil {
		t.Fatal("expected error")
	}
	if g.Stats().Generation != 2 {
		t.Fatal("failed batch changed generation")
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	// Overshoot mid-batch but return under the limit by the end: allowed.
	if _, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""},
	}}); err != nil {
		t.Fatal(err)
	}
	// Final state over the limit: whole batch rolls back.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("capacity failure leaked state")
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); err != ErrCycle {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestEdgeCapacityAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 2, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "c"}}}); err != nil {
		t.Fatal(err)
	}
	// Swap within the limit is fine; overshooting the final count is not.
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "c"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); err != ErrCapacity {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 2 {
		t.Fatal("capacity failure leaked state")
	}
}

func TestDeleteNodeCascadeAndTransitiveCycle(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); err != ErrCycle {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 {
		t.Fatalf("incident edges not cascaded: %+v", s.Edges)
	}
	// After the cascade, c -> a is legal again.
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotIsolationAndOrder(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "a"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatalf("nodes not sorted: %v", s.Nodes)
		}
	}
	if s.Edges[0] != (Edge{"a", "b"}) || s.Edges[1] != (Edge{"c", "a"}) {
		t.Fatalf("edges not sorted: %v", s.Edges)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependenceAndClock(t *testing.T) {
	g := graph(t)
	r, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if c.Stats().Generation != r.Generation {
		t.Fatal("clone lost logical clock")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	if len(g.Snapshot().Edges) != 1 || len(c.Snapshot().Edges) != 0 {
		t.Fatal("clone and original share state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			m := fmt.Sprintf("n%d", (i+1)%32)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Reachable(n, m)
			_ = g.Snapshot()
			_ = g.Stats()
		}()
	}
	w.Wait()
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	st := g.Stats()
	s := g.Snapshot()
	if st.Nodes != len(s.Nodes) || st.Edges != len(s.Edges) || st.Generation != s.Generation {
		t.Fatal("stats and snapshot disagree")
	}
	if c.Stats() != st {
		t.Fatal("clone diverged")
	}
	if got := len(g.Snapshot().Nodes); got != 32 {
		t.Fatal(got)
	}
}

func TestConcurrentCloneAndApply(t *testing.T) {
	g, _ := New(Options{MaxNodes: 64, MaxEdges: 64, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		w.Add(2)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("w%d", i)
			for j := 0; j < 10; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}, {DeleteNode, n, ""}}})
			}
		}()
		go func() {
			defer w.Done()
			for j := 0; j < 10; j++ {
				if c, err := g.Clone(); err == nil {
					_ = c.Stats()
				}
			}
		}()
	}
	w.Wait()
	if !errors.Is(func() error { _, e := g.Apply(Batch{}); return e }(), nil) {
		t.Fatal("graph unusable after concurrent clone")
	}
}
