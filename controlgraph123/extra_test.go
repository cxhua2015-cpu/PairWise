package controlgraph123

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}, {1, -1, 1}, {1, 1, -1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	for _, bad := range []string{"", "A", "a b", "a.b", "é", "toolongname", "a/b"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, bad, ""}}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%q: %v", bad, e)
		}
	}
	for _, good := range []string{"a", "z-0_9", "12345678"} {
		if _, e := g.Apply(Batch{Ops: []Op{{AddNode, good, ""}}}); e != nil {
			t.Fatalf("%q: %v", good, e)
		}
	}
}

func TestUnknownKindAndExtraFields(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: 0, From: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{Kind: 99, From: "a"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", "b"}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", ""}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	// Structural validation must happen before any state read: the bad op
	// poisons the whole batch even though earlier ops would succeed.
	if _, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 0}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Nodes) != 0 {
		t.Fatal("batch must be rolled back")
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
	must(Batch{Ops: []Op{{DeleteEdge, "a", "zz"}}}, ErrNotFound)
	must(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}}, nil)
	must(Batch{Ops: []Op{{AddEdge, "a", "b"}}}, ErrExists)
	must(Batch{Ops: []Op{{DeleteEdge, "a", "b"}, {DeleteEdge, "a", "b"}}}, ErrNotFound)
}

func TestSelfLoopAndIndirectCycle(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""}, {AddNode, "d", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"},
	}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "c", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	// Diamond without cycle is fine.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "d"}, {AddEdge, "d", "c"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadeAndRollback(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}}); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatal(s.Edges)
	}
	// Failed batch (capacity at end) must roll back entirely.
	before := g.Snapshot()
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "d", ""}, {AddNode, "e", ""}, {AddNode, "f", ""}, {AddNode, "g", ""}}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("rollback failed")
	}
}

func TestCapacityAtBatchEnd(t *testing.T) {
	g, e := New(Options{MaxNodes: 3, MaxEdges: 1, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	// Transiently exceeds capacity inside the batch but ends within limits.
	if _, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"},
	}}); e != nil {
		t.Fatal(e)
	}
	// Edge count only exceeds the limit at batch end.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "c"}}}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	// A batch that transiently exceeds edge capacity but deletes in time is fine.
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "c"}, {DeleteEdge, "a", "b"}}}); e != nil {
		t.Fatal(e)
	}
}

func TestGenerationAndEmptyBatch(t *testing.T) {
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
	// Failed batch does not bump generation.
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if g.Snapshot().Generation != 1 {
		t.Fatal()
	}
	r, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "c", ""}}})
	if r.Generation != 2 {
		t.Fatal(r)
	}
}

func TestSnapshotSortedAndIsolated(t *testing.T) {
	g := graph(t)
	if _, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "c", ""}, {AddNode, "a", ""}, {AddNode, "b", ""},
		{AddEdge, "c", "b"}, {AddEdge, "a", "c"}, {AddEdge, "a", "b"},
	}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if !sort.StringsAreSorted(s.Nodes) {
		t.Fatal(s.Nodes)
	}
	want := []Edge{{"a", "b"}, {"a", "c"}, {"c", "b"}}
	if !reflect.DeepEqual(s.Edges, want) {
		t.Fatal(s.Edges)
	}
	// Mutating the returned snapshot must not affect internal state.
	s.Nodes[0] = "zz"
	s.Edges[0] = Edge{"zz", "zz"}
	s2 := g.Snapshot()
	if s2.Nodes[0] == "zz" || s2.Edges[0].From == "zz" {
		t.Fatal("snapshot not isolated")
	}
}

func TestReachableErrors(t *testing.T) {
	g := graph(t)
	if _, e := g.Reachable("BAD!", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
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
	g, e := New(Options{MaxNodes: 1000, MaxEdges: 1000, MaxNameBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		w.Add(1)
		go func() {
			defer w.Done()
			n := "n" + strconv.Itoa(i)
			for j := 0; j < 50; j++ {
				m := "m" + strconv.Itoa(j)
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, m, ""}, {AddEdge, n, m}}})
				_, _ = g.Reachable(n, m)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, m}}})
			}
		}()
	}
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) == 0 || len(s.Edges) != 0 {
		t.Fatal(len(s.Nodes), len(s.Edges))
	}
	if !sort.StringsAreSorted(s.Nodes) {
		t.Fatal("unsorted")
	}
}

func TestConcurrentDAGInvariant(t *testing.T) {
	// Writers add edges in both directions between ordered pairs; whichever
	// direction wins, the graph must remain acyclic and consistent.
	g, e := New(Options{MaxNodes: 64, MaxEdges: 512, MaxNameBytes: 8})
	if e != nil {
		t.Fatal(e)
	}
	names := make([]string, 8)
	ops := make([]Op, 0, len(names))
	for i := range names {
		names[i] = fmt.Sprintf("n%d", i)
		ops = append(ops, Op{AddNode, names[i], ""})
	}
	if _, e := g.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	var w sync.WaitGroup
	for i := 0; i < len(names); i++ {
		for j := 0; j < len(names); j++ {
			if i == j {
				continue
			}
			i, j := i, j
			w.Add(1)
			go func() {
				defer w.Done()
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, names[i], names[j]}}})
			}()
		}
	}
	w.Wait()
	s := g.Snapshot()
	// Verify acyclicity of the snapshot via Kahn's algorithm.
	indeg := map[string]int{}
	adj := map[string][]string{}
	for _, e := range s.Edges {
		indeg[e.To]++
		adj[e.From] = append(adj[e.From], e.To)
	}
	var queue []string
	for _, n := range s.Nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	seen := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		seen++
		for _, m := range adj[n] {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if seen != len(s.Nodes) {
		t.Fatal("cycle detected in snapshot")
	}
}
