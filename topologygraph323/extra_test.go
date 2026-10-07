package topologygraph323

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
	bad := []string{"", "A", "a b", "a.b", "中文", "toolongname", "a/b"}
	for _, n := range bad {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", n, e)
		}
	}
	for _, n := range []string{"a", "z-9_", "0"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}}); e != nil {
			t.Fatalf("%q: %v", n, e)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Unknown kind and extra fields must fail even if earlier ops are fine.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind(99), "b", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "x"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("state mutated by invalid batch")
	}
}

func TestRollbackOnError(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}, {AddEdge, "b", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Generation != 0 {
		t.Fatalf("not rolled back: %+v", s)
	}
}

func TestCapacityRollback(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if n := len(g.Snapshot().Nodes); n != 0 {
		t.Fatal(n)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "b", "b"}}}); !errors.Is(e, ErrCycle) {
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
	must(Batch{Ops: []Op{{DeleteNode, "b", ""}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if r.Generation != 1 {
		t.Fatal(r)
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatal(r)
	}
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) || g.Snapshot().Generation != 2 {
		t.Fatal(e)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b", "c"}) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	s.Nodes[0] = "zzz"
	s.Edges[0] = Edge{"z", "z"}
	if g.Snapshot().Nodes[0] != "a" || g.Snapshot().Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("A", "b"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
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
			n := fmt.Sprintf("n-%d", i)
			m := fmt.Sprintf("n-%d", (i+1)%16)
			for j := 0; j < 50; j++ {
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
	if len(s.Nodes) != 16 || s.Generation == 0 {
		t.Fatalf("%+v", s)
	}
	for _, e := range s.Edges {
		// no cycle may ever be observable
		seen := map[string]bool{}
		cur := e.To
		for cur != e.From {
			if seen[cur] {
				t.Fatal("cycle in snapshot")
			}
			seen[cur] = true
			next := ""
			for _, e2 := range s.Edges {
				if e2.From == cur {
					next = e2.To
					break
				}
			}
			if next == "" {
				break
			}
			cur = next
		}
	}
}
