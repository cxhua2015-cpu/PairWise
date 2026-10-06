package topologygraph288

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); err != ErrInvalidOptions {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "é", "toolongname", "a.b"}
	for _, n := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != ErrInvalidInput {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-nam_1", ""}}}); err != nil {
		t.Fatal(err)
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{Kind(99), "a", ""}}}); err != ErrInvalidInput {
		t.Fatal("unknown kind")
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "a", "b"}}}); err != ErrInvalidInput {
		t.Fatal("extra field on node op")
	}
}

func TestGenerationSemantics(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatalf("generation=%d", r.Generation)
	}
	// Failed batch must not bump generation.
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != ErrExists {
		t.Fatal(err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}}})
	if r.Generation != 2 {
		t.Fatalf("generation=%d", r.Generation)
	}
	if g.Stats().Generation != 2 {
		t.Fatal("stats generation")
	}
}

func TestBatchRollback(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {DeleteNode, "zz", ""}}})
	if err != ErrNotFound {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("batch not rolled back")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if err != ErrCapacity {
		t.Fatal(err)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatalf("capacity failure leaked %d nodes", n)
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "c", "b"}, {DeleteNode, "b", ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := g.Snapshot()
	if len(s.Edges) != 0 || len(s.Nodes) != 2 {
		t.Fatalf("cascade: %+v", s)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "b", "c"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatalf("nodes: %v", s.Nodes)
		}
	}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	for i, e := range wantEdges {
		if s.Edges[i] != e {
			t.Fatalf("edges: %v", s.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0] != (Edge{"a", "b"}) {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); err != ErrNotFound {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, err := g.Reachable("a", "a"); err != nil {
		t.Fatal(err)
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
		t.Fatal("clone aliases original")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n-%d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n + "x", ""}, {AddEdge, n, n + "x"}}})
			_, _ = g.Reachable(n, n+"x")
			_ = g.Snapshot()
			_ = g.Stats()
			_ = g.ValidateBatch(Batch{Ops: []Op{{DeleteNode, n, ""}}})
		}()
	}
	wg.Wait()
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	st := c.Stats()
	if st.Nodes != 32 || st.Edges != 16 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestConcurrentApplySerializable(t *testing.T) {
	g, _ := New(Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok, exists int
	for err := range errs {
		if err == nil {
			ok++
		} else if errors.Is(err, ErrExists) {
			exists++
		}
	}
	if ok != 1 || exists != 7 {
		t.Fatalf("ok=%d exists=%d", ok, exists)
	}
	if g.Stats().Generation != 1 {
		t.Fatal("generation must bump once")
	}
}
