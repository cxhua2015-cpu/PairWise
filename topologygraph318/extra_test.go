package topologygraph318

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
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
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for i, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	if g.Snapshot().Generation != 0 {
		t.Fatal("generation changed on invalid input")
	}
}

func TestEmptyBatch(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(e, r)
	}
}

func TestGenerationIncrementsOnce(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(e, r)
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteNode, "a", ""}, {AddEdge, "c", "zzz"}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	after := g.Snapshot()
	if before.Generation != after.Generation || len(after.Nodes) != 2 || len(after.Edges) != 1 {
		t.Fatalf("not rolled back: %+v", after)
	}
}

func TestDuplicateAndMissing(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "a", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	// Temporarily exceeds capacity mid-batch, fine at the end.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {DeleteNode, "c", ""}}})
	if e != nil {
		t.Fatal(e)
	}
	// Exceeds at the end: full rollback.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) || len(g.Snapshot().Nodes) != 2 {
		t.Fatal(e)
	}
	// Edge capacity.
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) && !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("edges leaked")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if s.Nodes[0] != "a" || s.Nodes[1] != "b" || s.Edges[0] != (Edge{"a", "b"}) {
		t.Fatalf("not sorted: %+v", s)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 16})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%02d", i)
			m := fmt.Sprintf("n%02d", (i+1)%16)
			for k := 0; k < 50; k++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) > 16 {
		t.Fatal(len(s.Nodes))
	}
	for _, e := range s.Edges {
		if e.From == e.To {
			t.Fatal("self loop")
		}
	}
}
