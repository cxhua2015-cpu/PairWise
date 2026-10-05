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
	prev := ""
	for i := 0; i < 8; i++ {
		n := fmt.Sprintf("n%02d", i)
		ops = append(ops, node(AddNode, n, "x"))
		if prev != "" {
			ops = append(ops, edge(AddEdge, prev, n))
		}
		prev = n
	}
	if _, e := s.Apply(Batch{Ops: ops}); e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	if _, e := s.Apply(Batch{Ops: []Op{edge(AddEdge, "n07", "n00")}}); !errors.Is(e, ErrCycle) {
		t.Fatalf("e=%v", e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{edge(AddEdge, "n07", "n05")}}); !errors.Is(e, ErrCycle) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("state mutated after cycle rollback")
	}
	// Diamond: a->b->d, a->c->d; adding d->a must cycle.
	if _, e := s.Apply(Batch{Ops: []Op{edge(AddEdge, "n00", "n07")}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Apply(Batch{Ops: []Op{edge(AddEdge, "n07", "n00")}}); !errors.Is(e, ErrCycle) {
		t.Fatalf("e=%v", e)
	}
}

func TestRemoveEdgeThenDeleteNodeSameBatch(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a"), node(AddNode, "b", "b"), edge(AddEdge, "a", "b")}})
	// Delete with live edge must conflict and roll back.
	if _, e := s.Apply(Batch{Ops: []Op{{Kind: DeleteNode, Name: "b"}}}); !errors.Is(e, ErrConflict) {
		t.Fatalf("e=%v", e)
	}
	// Remove edge then delete node in one batch succeeds.
	x, e := s.Apply(Batch{Ops: []Op{edge(RemoveEdge, "a", "b"), {Kind: DeleteNode, Name: "b"}}})
	if e != nil || x.Revision != 5 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if len(x.ChangedEdges) != 0 {
		t.Fatal("removed edge must not survive in ChangedEdges")
	}
	snap := s.Snapshot()
	if len(snap.Nodes) != 1 || len(snap.Edges) != 0 {
		t.Fatal(snap)
	}
	// Re-add b and re-wire: node reuse after delete.
	x, e = s.Apply(Batch{Ops: []Op{node(AddNode, "b", "b2"), edge(AddEdge, "a", "b")}})
	if e != nil || x.Revision != 7 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if ok, _ := s.Reachable("a", "b"); !ok {
		t.Fatal("expected reachability after re-add")
	}
}

func TestRevisionRollbackOnCapacity(t *testing.T) {
	s, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 8})
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "aa")}})
	before := s.Snapshot()
	// Temporarily valid ops, but final node count exceeds MaxNodes.
	_, e := s.Apply(Batch{Ops: []Op{node(AddNode, "b", "b"), node(AddNode, "c", "c")}})
	if !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("snapshot changed after capacity rollback")
	}
	// Next successful batch must continue revision sequence (2), not skip.
	x, e := s.Apply(Batch{Ops: []Op{node(AddNode, "b", "b")}})
	if e != nil || x.Revision != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	// Temporary overflow but final compliance: add two, delete one.
	s2, _ := New(Options{MaxNodes: 2, MaxEdges: 4, MaxNameBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 8})
	_, _ = s2.Apply(Batch{Ops: []Op{node(AddNode, "a", "aa")}})
	x, e = s2.Apply(Batch{Ops: []Op{node(AddNode, "b", "bb"), node(AddNode, "c", "cc"), {Kind: DeleteNode, Name: "c"}}})
	if e != nil || x.Revision != 4 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if got := len(s2.Snapshot().Nodes); got != 2 {
		t.Fatal(got)
	}
	// Total payload bytes overflow rolls back.
	s3, _ := New(Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8, MaxPayloadBytes: 4, MaxTotalPayloadBytes: 5})
	_, _ = s3.Apply(Batch{Ops: []Op{node(AddNode, "a", "aaa")}})
	if _, e := s3.Apply(Batch{Ops: []Op{node(AddNode, "b", "bbb")}}); !errors.Is(e, ErrCapacity) {
		t.Fatalf("e=%v", e)
	}
	x, e = s3.Apply(Batch{Ops: []Op{node(AddNode, "b", "bb")}})
	if e != nil || x.Revision != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}

func TestPayloadOwnershipDeep(t *testing.T) {
	s := ds(t)
	in := []byte("orig")
	x, _ := s.Apply(Batch{Ops: []Op{{Kind: AddNode, Name: "a", Payload: in}}})
	in[0] = 'X'
	x.ChangedNodes[0].Payload[1] = 'Y'
	x2, _ := s.Apply(Batch{Ops: []Op{{Kind: UpdateNode, Name: "a", Payload: []byte("next")}}})
	x2.ChangedNodes[0].Payload[0] = 'Z'
	got := s.Snapshot().Nodes[0].Payload
	if string(got) != "next" {
		t.Fatalf("got %q", got)
	}
	got[0] = 'Q'
	if string(s.Snapshot().Nodes[0].Payload) != "next" {
		t.Fatal("snapshot aliases store payload")
	}
}

func TestStableTopological(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{
		node(AddNode, "d", "d"), node(AddNode, "b", "b"), node(AddNode, "a", "a"), node(AddNode, "c", "c"),
		edge(AddEdge, "a", "d"), edge(AddEdge, "b", "d"), edge(AddEdge, "c", "d"),
	}})
	for i := 0; i < 20; i++ {
		if got := s.Topological(); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
			t.Fatal(got)
		}
	}
	// Two independent chains: a->c, b->d => lexicographic: a,b,c,d.
	s2 := ds(t)
	_, _ = s2.Apply(Batch{Ops: []Op{
		node(AddNode, "d", "d"), node(AddNode, "c", "c"), node(AddNode, "b", "b"), node(AddNode, "a", "a"),
		edge(AddEdge, "a", "c"), edge(AddEdge, "b", "d"),
	}})
	if got := s2.Topological(); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatal(got)
	}
	// Empty store.
	if got := ds(t).Topological(); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestReachableValidation(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a")}})
	if _, e := s.Reachable("a", "bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := s.Reachable("a", "missing"); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	ok, e := s.Reachable("a", "a")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	s, _ := New(Options{MaxNodes: 64, MaxEdges: 128, MaxNameBytes: 16, MaxPayloadBytes: 16, MaxTotalPayloadBytes: 1024})
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
				_, _ = s.Reachable(n, n)
				_ = s.Topological()
				_ = s.Snapshot()
			}
		}()
	}
	wg.Wait()
	if got := len(s.Snapshot().Nodes); got != 8 {
		t.Fatal(got)
	}
}

func TestEmptyBatchNoGeneration(t *testing.T) {
	s := ds(t)
	x, e := s.Apply(Batch{})
	if e != nil || x.Generation != 0 || x.Revision != 0 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if s.Snapshot().Generation != 0 {
		t.Fatal("empty batch must not bump generation")
	}
}
