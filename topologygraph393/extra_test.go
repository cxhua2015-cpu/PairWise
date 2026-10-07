package topologygraph393

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -2, 3},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	g := graph(t)
	cases := []Op{
		{Kind(0), "a", ""},           // unknown kind
		{Kind(99), "a", ""},          // unknown kind
		{AddNode, "", ""},            // empty name
		{AddNode, "A", ""},           // uppercase
		{AddNode, "a b", ""},         // space
		{AddNode, "a.b", ""},         // dot
		{AddNode, "é", ""},           // non-ASCII
		{AddNode, "toolongname", ""}, // exceeds MaxNameBytes=8
		{AddNode, "a", "b"},          // extra field on node op
		{DeleteNode, "a", "b"},       // extra field on node op
		{AddEdge, "a", ""},           // missing To
		{AddEdge, "", "b"},           // missing From
		{DeleteEdge, "a", "Bad"},     // invalid To
	}
	for _, op := range cases {
		_, e := g.Apply(Batch{Ops: []Op{op}})
		if !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	// Validation happens before any state read: a batch whose first op is
	// structurally invalid must fail even if later ops would also fail.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "ok", ""}, {Kind(7), "x", ""}}})
	if !errors.Is(e, ErrInvalidInput) || len(g.Snapshot().Nodes) != 0 {
		t.Fatal(e)
	}
	// Boundary: exactly MaxNameBytes is accepted.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "abcd-123", ""}}}); e != nil {
		t.Fatal(e)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	r, e = g.Apply(Batch{Ops: nil})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(r, e)
	}
	// Failed batch leaves generation untouched.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal(g.Snapshot().Generation)
	}
}

func TestExistsAndNotFound(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "ghost", ""}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
}

func TestSelfLoopIsCycle(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
}

func TestAtomicRollbackOnCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	before := g.Snapshot()
	// Batch exceeds final node capacity: nothing may be applied.
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Edge capacity exceeded at end: rollback, including intermediate deletes.
	g, _ = New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddEdge, "a", "b"}}})
	before = g.Snapshot()
	_, e = g.Apply(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {AddEdge, "a", "b"}, {AddEdge, "b", "c"}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal(e)
	}
	// Transient overflow inside the batch is fine if the final state fits.
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {DeleteNode, "d", ""}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
		{DeleteNode, "b", ""},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	want := Snapshot{Generation: 1, Nodes: []string{"a", "c"}, Edges: []Edge{{"a", "c"}}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got %+v want %+v", s, want)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g, _ := New(Options{MaxNodes: 16, MaxEdges: 16, MaxNameBytes: 8})
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "z", ""}, {AddNode, "a", ""}, {AddNode, "m", ""},
		{AddEdge, "a", "m"}, {AddEdge, "a", "z"}, {AddEdge, "m", "z"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "m", "z"}) {
		t.Fatal(s.Nodes)
	}
	wantEdges := []Edge{{"a", "m"}, {"a", "z"}, {"m", "z"}}
	if !reflect.DeepEqual(s.Edges, wantEdges) {
		t.Fatal(s.Edges)
	}
	// Mutating the returned snapshot must not affect internal state.
	s.Nodes[0] = "corrupt"
	s.Edges[0].From = "corrupt"
	s2 := g.Snapshot()
	if s2.Nodes[0] != "a" || s2.Edges[0].From != "a" {
		t.Fatal("snapshot shares memory with graph")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Reachable("a", "ghost"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("ghost", "a"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("a", "Bad Name"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	ok, e := g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
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
			n := fmt.Sprintf("n%02d", i)
			m := fmt.Sprintf("n%02d", (i+1)%16)
			for k := 0; k < 20; k++ {
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
	if len(s.Nodes) != 16 || len(s.Edges) > 128 {
		t.Fatal(len(s.Nodes), len(s.Edges))
	}
}
