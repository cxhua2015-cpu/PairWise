package dagstore

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestDeepCycleDetection(t *testing.T) {
	s := ds(t)
	var ops []Op
	names := []string{"a", "b", "c", "d", "e", "f"}
	for _, n := range names {
		ops = append(ops, node(AddNode, n, "x"))
	}
	for i := 0; i+1 < len(names); i++ {
		ops = append(ops, edge(AddEdge, names[i], names[i+1]))
	}
	if _, e := s.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	// closing the long chain f->...->a must be detected as a cycle
	if _, e := s.Apply(Batch{Ops: []Op{edge(AddEdge, "f", "a")}}); !errors.Is(e, ErrCycle) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state mutated after cycle rollback")
	}
	// a non-cycle back-edge into a sibling branch is fine
	if _, e := s.Apply(Batch{Ops: []Op{node(AddNode, "g", "g"), edge(AddEdge, "g", "f")}}); e != nil {
		t.Fatal(e)
	}
	if ok, _ := s.Reachable("a", "f"); !ok {
		t.Fatal("a should reach f")
	}
	if ok, _ := s.Reachable("f", "a"); ok {
		t.Fatal("f must not reach a")
	}
}

func TestEdgeRemovalThenNodeDeleteSameBatch(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a"), node(AddNode, "b", "b"), edge(AddEdge, "a", "b")}})
	// deleting b while its edge still exists conflicts and rolls back
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: DeleteNode, Name: "b"}}}); !errors.Is(e, ErrConflict) {
		t.Fatalf("e=%v", e)
	}
	// removing the edge first in the same batch makes the delete legal
	x, e := s.Apply(Batch{Ops: []Op{edge(RemoveEdge, "a", "b"), {Kind: DeleteNode, Name: "b"}}})
	if e != nil {
		t.Fatal(e)
	}
	if len(x.ChangedEdges) != 0 || len(x.ChangedNodes) != 0 {
		t.Fatalf("removed objects must not survive: %+v", x)
	}
	if len(s.Snapshot().Edges) != 0 || len(s.Snapshot().Nodes) != 1 {
		t.Fatal(s.Snapshot())
	}
}

func TestRevisionNotConsumedOnRollback(t *testing.T) {
	s := ds(t)
	x, _ := s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a")}})
	if x.Revision != 1 {
		t.Fatal(x)
	}
	// failing batch allocates revisions internally but must roll them back
	_, e := s.Apply(Batch{Ops: []Op{node(AddNode, "b", "b"), node(AddNode, "c", "c"), node(AddNode, "a", "dup")}})
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("e=%v", e)
	}
	snap := s.Snapshot()
	if snap.NextRevision != 2 || snap.Generation != 1 {
		t.Fatalf("snap=%+v", snap)
	}
	x, e = s.Apply(Batch{Ops: []Op{node(AddNode, "d", "d")}})
	if e != nil || x.Revision != 2 || x.Generation != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestFinalCapacityTemporaryOverflow(t *testing.T) {
	s, _ := New(Options{MaxNodes: 2, MaxEdges: 2, MaxNameBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 6})
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "aaaa"), node(AddNode, "b", "bb")}})
	// temporarily 3 nodes and 8 payload bytes, final state compliant
	x, e := s.Apply(Batch{Ops: []Op{node(AddNode, "c", "cc"), {Kind: DeleteNode, Name: "a"}}})
	if e != nil {
		t.Fatal(e)
	}
	if x.Revision != 4 {
		t.Fatal(x)
	}
	snap := s.Snapshot()
	if len(snap.Nodes) != 2 {
		t.Fatal(snap)
	}
	// final payload overflow must fail with ErrCapacity and roll back
	_, e = s.Apply(Batch{Ops: []Op{node(UpdateNode, "b", "bbbb"), node(UpdateNode, "c", "ccc")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if s.Snapshot().NextRevision != 5 {
		t.Fatal("revision consumed by failed batch")
	}
}

func TestPayloadOwnershipAllPaths(t *testing.T) {
	s := ds(t)
	in := []byte("in")
	x, _ := s.Apply(Batch{Ops: []Op{{Kind: AddNode, Name: "a", Payload: in}}})
	in[0] = 'X'
	if string(x.ChangedNodes[0].Payload) != "in" {
		t.Fatal("input aliased")
	}
	x.ChangedNodes[0].Payload[0] = 'Y'
	if string(s.Snapshot().Nodes[0].Payload) != "in" {
		t.Fatal("result aliased to store")
	}
	up := []byte("up")
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: UpdateNode, Name: "a", Payload: up}}})
	up[0] = 'Z'
	s1 := s.Snapshot()
	s1.Nodes[0].Payload[0] = 'W'
	if string(s.Snapshot().Nodes[0].Payload) != "up" {
		t.Fatal("snapshot aliased")
	}
}

func TestStableTopologicalOrder(t *testing.T) {
	s := ds(t)
	ops := []Op{
		node(AddNode, "delta", "d"), node(AddNode, "alpha", "a"),
		node(AddNode, "charlie", "c"), node(AddNode, "bravo", "b"),
		edge(AddEdge, "delta", "charlie"), edge(AddEdge, "alpha", "bravo"),
		edge(AddEdge, "bravo", "charlie"),
	}
	if _, e := s.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	want := []string{"alpha", "bravo", "delta", "charlie"}
	for i := 0; i < 3; i++ {
		if got := s.Topological(); !reflect.DeepEqual(got, want) {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
	// snapshot ordering is stable and sorted
	snap := s.Snapshot()
	names := []string{snap.Nodes[0].Name, snap.Nodes[1].Name, snap.Nodes[2].Name, snap.Nodes[3].Name}
	if !reflect.DeepEqual(names, []string{"alpha", "bravo", "charlie", "delta"}) {
		t.Fatal(names)
	}
	if snap.Edges[0].From != "alpha" || snap.Edges[1].From != "bravo" || snap.Edges[2].From != "delta" {
		t.Fatal(snap.Edges)
	}
}

func TestEmptyBatch(t *testing.T) {
	s := ds(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a")}})
	x, e = s.Apply(Batch{})
	if e != nil || x.Generation != 1 || x.Revision != 1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestReachableValidation(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a")}})
	if _, e := s.Reachable("bad?", "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Reachable("a", "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if ok, e := s.Reachable("a", "a"); e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxNodes: 64, MaxEdges: 128, MaxNameBytes: 16, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := fmt.Sprintf("n%d", i)
			for j := 0; j < 20; j++ {
				_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, n, "v")}})
				_, _ = s.Apply(Batch{Ops: []Op{node(UpdateNode, n, "w")}})
				_ = s.Topological()
				_ = s.Snapshot()
				_, _ = s.Reachable(n, n)
			}
		}()
	}
	wg.Wait()
	if got := len(s.Snapshot().Nodes); got != 8 {
		t.Fatal(got)
	}
}
