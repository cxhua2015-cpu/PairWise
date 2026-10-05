package controlgraph178

import (
	"errors"
	"fmt"
	"reflect"
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

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []Op{
		{Kind: AddNode, From: ""},
		{Kind: AddNode, From: "A"},
		{Kind: AddNode, From: "a b"},
		{Kind: AddNode, From: "a.b"},
		{Kind: AddNode, From: "toolongname"},
		{Kind: AddNode, From: "a", To: "x"},
		{Kind: DeleteNode, From: "a", To: "x"},
		{Kind: AddEdge, From: "a"},
		{Kind: DeleteEdge, From: "", To: "b"},
		{Kind: Kind(0), From: "a"},
		{Kind: Kind(99), From: "a"},
	}
	for _, op := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "ok_1-2"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Reachable("BAD", "ok_1-2"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; first op must not be applied.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: Kind(0)}}})
	if !errors.Is(e, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	must := func(b Batch, want error) {
		t.Helper()
		if _, e := g.Apply(b); !errors.Is(e, want) {
			t.Fatalf("want %v got %v", want, e)
		}
	}
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, nil)
	must(Batch{Ops: []Op{{AddNode, "a", ""}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteNode, "zz", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "zz", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "a"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, nil)
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestAtomicRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// Exceeds node capacity only at the end -> whole batch rolls back.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Error mid-batch (cycle) also rolls back earlier ops.
	_, e = g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	if g.Snapshot().Generation != 0 {
		t.Fatal()
	}
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 || g.Snapshot().Generation != 0 {
		t.Fatal(r, e)
	}
	r, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch must not bump generation.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal(g.Snapshot().Generation)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b"}) ||
		!reflect.DeepEqual(s.Edges, []Edge{{"a", "b"}}) {
		t.Fatal(s)
	}
	s.Nodes[0] = "mut"
	s.Edges[0].From = "mut"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableMissing(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, _ := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	var w sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 20; j++ {
				m := fmt.Sprintf("n%d", (i+j)%16)
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
	if len(s.Nodes) > 128 || len(s.Edges) > 256 {
		t.Fatal("capacity violated")
	}
	// Graph must remain acyclic: Kahn's algorithm.
	indeg := map[string]int{}
	out := map[string][]string{}
	for _, e := range s.Edges {
		indeg[e.To]++
		out[e.From] = append(out[e.From], e.To)
	}
	var q []string
	for _, n := range s.Nodes {
		if indeg[n] == 0 {
			q = append(q, n)
		}
	}
	seen := 0
	for len(q) > 0 {
		n := q[0]
		q = q[1:]
		seen++
		for _, m := range out[n] {
			indeg[m]--
			if indeg[m] == 0 {
				q = append(q, m)
			}
		}
	}
	if seen != len(s.Nodes) {
		t.Fatal("cycle detected after concurrent adds")
	}
}
