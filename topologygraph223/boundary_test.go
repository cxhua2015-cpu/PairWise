package topologygraph223

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts %+v: %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a/b", "a.b", "é", "toolongname"}
	for _, n := range bad {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: %v", n, err)
		}
	}
	for _, n := range []string{"a", "z-0_9", "abcdefgh"} {
		if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, n, ""}}}); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind(0), "a", ""}}},
		{Ops: []Op{{Kind(99), "a", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{DeleteNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
		{Ops: []Op{{AddEdge, "a", "a"}}},
	}
	for i, b := range cases {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
		if _, err := g.Apply(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("apply case %d: %v", i, err)
		}
	}
	if got := g.Snapshot().Generation; got != 0 {
		t.Fatalf("generation moved: %d", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); err != nil {
		t.Fatal(err)
	}
	r, err = g.Apply(Batch{})
	if err != nil || r.Generation != 1 {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	before := g.Snapshot()
	_, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"},
		{AddEdge, "b", "a"}, // cycle: whole batch must roll back
	}})
	if !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("not rolled back: %+v", got)
	}
}

func TestCapacityCheckedAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	// Net zero nodes but transiently above capacity: must succeed.
	if _, err := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""},
	}}); err != nil {
		t.Fatal(err)
	}
	// Final state above capacity: must fail and roll back.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if len(g.Snapshot().Nodes) != 2 {
		t.Fatal("rolled back expected")
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		_, err := g.Apply(b)
		if !errors.Is(err, want) {
			t.Fatalf("want %v got %v", want, err)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestReachableErrorsAndSelf(t *testing.T) {
	g := graph(t)
	if _, err := g.Reachable("a", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}})
	s := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	for i, n := range wantNodes {
		if s.Nodes[i] != n {
			t.Fatalf("nodes: %v", s.Nodes)
		}
	}
	wantEdges := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	for i, e := range wantEdges {
		if s.Edges[i] != e {
			t.Fatalf("edges: %v", s.Edges)
		}
	}
	// Mutating returned slices must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	if g.Snapshot().Nodes[0] != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestCloneIndependentClockAndState(t *testing.T) {
	g := graph(t)
	r, _ := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Generation != r.Generation || got.Nodes != 2 || got.Edges != 1 {
		t.Fatalf("clone stats: %+v", got)
	}
	// Mutate original; clone must be unaffected.
	_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if s := c.Snapshot(); len(s.Nodes) != 2 || len(s.Edges) != 1 || s.Generation != r.Generation {
		t.Fatalf("clone changed: %+v", s)
	}
	ok, _ := c.Reachable("a", "b")
	if !ok {
		t.Fatal("clone lost edge")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 12})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n-%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, "n-00"}}})
			_, _ = g.Reachable("n-00", n)
			_ = g.Snapshot()
			_ = g.Stats()
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "n-00"}}})
			if i%4 == 0 {
				_, _ = g.Clone()
			}
		}()
	}
	wg.Wait()
	st := g.Stats()
	if st.Nodes != 16 {
		t.Fatalf("stats: %+v", st)
	}
}
