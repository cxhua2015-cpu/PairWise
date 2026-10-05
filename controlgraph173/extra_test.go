package controlgraph173

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

func TestInvalidInput(t *testing.T) {
	g := graph(t)
	cases := []Batch{
		{Ops: []Op{{Kind: 0, From: "a"}}},
		{Ops: []Op{{Kind: 99, From: "a"}}},
		{Ops: []Op{{AddNode, "", ""}}},
		{Ops: []Op{{AddNode, "A", ""}}},
		{Ops: []Op{{AddNode, "a b", ""}}},
		{Ops: []Op{{AddNode, "toolongname", ""}}},
		{Ops: []Op{{AddNode, "a", "extra"}}},
		{Ops: []Op{{AddEdge, "a", ""}}},
		{Ops: []Op{{DeleteEdge, "", "b"}}},
	}
	for _, b := range cases {
		if _, e := g.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatal("state mutated by invalid batches")
	}
}

func TestValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Second op is structurally invalid; first op would fail with ErrExists
	// only if state were read. Structural validation must win.
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {Kind: 42}}})
	if !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestRollbackAndGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}}})
	if e != nil || r.Generation != 1 {
		t.Fatal(e, r)
	}
	before := g.Snapshot()
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "c", "zzz"}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("failed batch mutated state")
	}
	r, e = g.Apply(Batch{})
	if e != nil || r.Generation != 1 {
		t.Fatal("empty batch changed generation")
	}
}

func TestExistsNotFound(t *testing.T) {
	g := graph(t)
	mk := func(ops ...Op) error { _, e := g.Apply(Batch{Ops: ops}); return e }
	if e := mk(Op{AddNode, "a", ""}, Op{AddNode, "a", ""}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteNode, "ghost", ""}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_ = mk(Op{AddNode, "a", ""}, Op{AddNode, "b", ""}, Op{AddEdge, "a", "b"})
	if e := mk(Op{AddEdge, "a", "b"}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteEdge, "b", "a"}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if e := mk(Op{AddEdge, "a", "ghost"}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSelfCycleAndCapacity(t *testing.T) {
	g, _ := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	mk := func(ops ...Op) error { _, e := g.Apply(Batch{Ops: ops}); return e }
	if e := mk(Op{AddNode, "a", ""}); e != nil {
		t.Fatal(e)
	}
	if e := mk(Op{AddEdge, "a", "a"}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	// Net-zero batch exceeding capacity mid-way must succeed.
	if e := mk(Op{AddNode, "b", ""}, Op{AddNode, "c", ""}, Op{DeleteNode, "c", ""}); e != nil {
		t.Fatal(e)
	}
	if e := mk(Op{AddNode, "c", ""}); !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if e := mk(Op{AddEdge, "a", "b"}, Op{AddEdge, "b", "a"}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if len(g.Snapshot().Edges) != 0 {
		t.Fatal("cycle batch leaked edges")
	}
}

func TestDeleteNodeCascadeAndReachable(t *testing.T) {
	g := graph(t)
	mk := func(ops ...Op) error { _, e := g.Apply(Batch{Ops: ops}); return e }
	if e := mk(Op{AddNode, "a", ""}, Op{AddNode, "b", ""}, Op{AddNode, "c", ""},
		Op{AddEdge, "a", "b"}, Op{AddEdge, "b", "c"}, Op{AddEdge, "a", "c"}); e != nil {
		t.Fatal(e)
	}
	if e := mk(Op{DeleteNode, "b", ""}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatal(s.Edges)
	}
	ok, e := g.Reachable("a", "c")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := g.Reachable("a", "ghost"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := g.Reachable("A", "c"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	ok, e = g.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal("self reachability", ok, e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddNode, "a", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	if !reflect.DeepEqual(s.Nodes, []string{"a", "b"}) {
		t.Fatal(s.Nodes)
	}
	s.Nodes[0] = "mutated"
	s.Edges[0].From = "mutated"
	again := g.Snapshot()
	if again.Nodes[0] != "a" || again.Edges[0].From != "a" {
		t.Fatal("snapshot aliases internal state")
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
			n := fmt.Sprintf("n%02d", i)
			for j := 0; j < 50; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{AddEdge, n, fmt.Sprintf("n%02d", (i+1)%16)}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteEdge, n, fmt.Sprintf("n%02d", (i+1)%16)}}})
			}
		}()
	}
	w.Wait()
	if got := len(g.Snapshot().Nodes); got != 16 {
		t.Fatal(got)
	}
}
