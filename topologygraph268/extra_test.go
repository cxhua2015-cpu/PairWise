package topologygraph268

import (
	"errors"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, err)
		}
	}
}

func TestValidateBatchStructural(t *testing.T) {
	g := graph(t)
	bad := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "b"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "a", "a"}}},
	}
	for i, b := range bad {
		if err := g.ValidateBatch(b); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddNode, "ok-1_", ""}}}); err != nil {
		t.Fatal(err)
	}
	// ValidateBatch must not mutate state.
	if s := g.Stats(); s.Nodes != 0 || s.Generation != 0 {
		t.Fatalf("validation had side effects: %+v", s)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{})
	if err != nil || r.Generation != 0 {
		t.Fatalf("%v %v", r, err)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r.Generation)
	}
	r, _ = g.Apply(Batch{})
	if r.Generation != 1 || g.Stats().Generation != 1 {
		t.Fatal(r.Generation)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	before := g.Snapshot()
	// Batch fails mid-way (duplicate add); nothing may be committed.
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != before.Generation || len(got.Nodes) != 1 {
		t.Fatalf("state changed: %+v", got)
	}
	// Capacity failure rolls back the whole batch.
	g2, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8})
	_, err = g2.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(err, ErrCapacity) || len(g2.Snapshot().Nodes) != 0 {
		t.Fatalf("%v %v", err, g2.Snapshot())
	}
}

func TestDeleteNodeCascadeAndErrors(t *testing.T) {
	g := graph(t)
	_, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {DeleteNode, "a", ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if s := g.Stats(); s.Nodes != 1 || s.Edges != 0 {
		t.Fatalf("%+v", s)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "zz"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestReachableErrorsAndSelf(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, err := g.Reachable("a", "a")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := g.Reachable("a", "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "BAD"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
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
		t.Fatal("clone diverges")
	}
	if _, err := c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); err != nil {
		t.Fatal(err)
	}
	if g.Stats().Edges != 1 || c.Stats().Edges != 0 {
		t.Fatal("clone aliases original")
	}
	if c.Stats().Generation != 2 || g.Stats().Generation != 1 {
		t.Fatal("logical clock not preserved/independent")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 128, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := string(rune('a' + i))
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
			_ = g.ValidateBatch(Batch{Ops: []Op{{AddEdge, n, "zz"}}})
			_ = g.Stats()
			_ = g.Snapshot()
			_, _ = g.Reachable(n, n)
			_, _ = g.Clone()
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 16 {
		t.Fatal(got)
	}
	// Concurrent writers: generation increments once per successful non-empty batch.
	g2, _ := New(Options{MaxNodes: 256, MaxEdges: 256, MaxNameBytes: 8})
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			_, _ = g2.Apply(Batch{Ops: []Op{{AddNode, string(rune('a'+i%26)) + string(rune('a'+i/26)), ""}}})
		}()
	}
	w.Wait()
	st := g2.Stats()
	if int(st.Generation) != st.Nodes {
		t.Fatalf("generation %d != nodes %d", st.Generation, st.Nodes)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if s.Nodes[0] != "a" || s.Nodes[1] != "b" {
		t.Fatalf("not sorted: %v", s.Nodes)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	g2 := g.Snapshot()
	if g2.Nodes[0] != "a" || g2.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}
