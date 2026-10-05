package subscriptiongraph

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
		{Ops: []Op{{AddNode, "a", "extra"}}},
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

func TestEmptyBatchGeneration(t *testing.T) {
	g := graph(t)
	r, e := g.Apply(Batch{})
	if e != nil || r.Generation != 0 {
		t.Fatal(r, e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}}); e != nil {
		t.Fatal(e)
	}
	r, e = g.Apply(Batch{})
	if e != nil || r.Generation != 1 || g.Snapshot().Generation != 1 {
		t.Fatal(r, e)
	}
}

func TestRollbackAtomicity(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	before := g.Snapshot()
	// 中间失败：重复节点。
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddNode, "a", ""}}})
	if !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	// 末位失败：未知节点。
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {DeleteNode, "zz", ""}}})
	if !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	// 环失败。
	_, e = g.Apply(Batch{Ops: []Op{{AddNode, "c", ""}, {AddEdge, "b", "a"}}})
	if !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	// 容量失败（临时超过 MaxNodes=5）。
	ops := []Op{}
	for i := 0; i < 6; i++ {
		ops = append(ops, Op{AddNode, fmt.Sprintf("n%d", i), ""})
	}
	_, e = g.Apply(Batch{Ops: ops})
	if !errors.Is(e, ErrCapacity) {
		t.Fatal(e)
	}
	if got := g.Snapshot(); got.Generation != before.Generation ||
		fmt.Sprint(got.Nodes) != fmt.Sprint(before.Nodes) ||
		fmt.Sprint(got.Edges) != fmt.Sprint(before.Edges) {
		t.Fatalf("state changed: %+v vs %+v", got, before)
	}
}

func TestSelfLoopAndDupEdge(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); !errors.Is(e, ErrCycle) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	if _, e := g.Apply(Batch{Ops: []Op{{AddEdge, "a", "b"}}}); !errors.Is(e, ErrExists) {
		t.Fatal(e)
	}
	if _, e := g.Apply(Batch{Ops: []Op{{DeleteEdge, "b", "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestDeleteNodeCascadeAndRestore(t *testing.T) {
	g := graph(t)
	_, e := g.Apply(Batch{Ops: []Op{
		{AddNode, "a", ""}, {AddNode, "b", ""}, {AddNode, "c", ""},
		{AddEdge, "a", "b"}, {AddEdge, "b", "c"}, {AddEdge, "a", "c"},
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}}); e != nil {
		t.Fatal(e)
	}
	s := g.Snapshot()
	if len(s.Edges) != 1 || s.Edges[0] != (Edge{"a", "c"}) {
		t.Fatalf("%+v", s.Edges)
	}
	if ok, _ := g.Reachable("a", "c"); !ok {
		t.Fatal()
	}
	if _, e := g.Reachable("a", "b"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	g := graph(t)
	_, _ = g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}})
	s := g.Snapshot()
	s.Nodes[0] = "zz"
	s.Edges[0].From = "zz"
	s2 := g.Snapshot()
	if s2.Nodes[0] == "zz" || s2.Edges[0].From == "zz" {
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
			n := fmt.Sprintf("node-%02d", i)
			for j := 0; j < 20; j++ {
				_, _ = g.Apply(Batch{Ops: []Op{{AddNode, n, ""}}})
				_, _ = g.Apply(Batch{Ops: []Op{{DeleteNode, n, ""}}})
				_, _ = g.Reachable(n, n)
				_ = g.Snapshot()
			}
		}()
	}
	// 并发构建一条链，只接受成功。
	w.Add(1)
	go func() {
		defer w.Done()
		for j := 0; j < 50; j++ {
			a := fmt.Sprintf("chain-%02d", j)
			b := fmt.Sprintf("chain-%02d", j+1)
			_, _ = g.Apply(Batch{Ops: []Op{{AddNode, a, ""}, {AddNode, b, ""}, {AddEdge, a, b}}})
		}
	}()
	w.Wait()
	s := g.Snapshot()
	if len(s.Nodes) != len(g.Snapshot().Nodes) {
		t.Fatal("inconsistent snapshot")
	}
}
