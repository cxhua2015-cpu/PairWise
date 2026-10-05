package dagstore

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func dop() Options {
	return Options{MaxNodes: 8, MaxEdges: 16, MaxNameBytes: 16, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 32}
}
func ds(t *testing.T) *Store {
	t.Helper()
	s, e := New(dop())
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func node(k OpKind, n, v string) Op { return Op{Kind: k, Name: n, Payload: []byte(v)} }
func edge(k OpKind, a, b string) Op { return Op{Kind: k, From: a, To: b} }
func TestValidationBeforeState(t *testing.T) {
	if _, e := New(Options{}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "x")}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{{Kind: DeleteNode, Name: "missing"}, {Kind: AddEdge, From: "bad?", To: "a"}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
}
func TestSequentialAddAndTopo(t *testing.T) {
	s := ds(t)
	x, e := s.Apply(Batch{Ops: []Op{node(AddNode, "c", "c"), node(AddNode, "a", "a"), node(AddNode, "b", "b"), edge(AddEdge, "a", "c"), edge(AddEdge, "b", "c")}})
	if e != nil || x.Revision != 5 || x.Generation != 1 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
	if got := s.Topological(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatal(got)
	}
	ok, e := s.Reachable("a", "c")
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
}
func TestCycleRollbackRevision(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a"), node(AddNode, "b", "b"), edge(AddEdge, "a", "b")}})
	before := s.Snapshot()
	_, e := s.Apply(Batch{Ops: []Op{edge(AddEdge, "b", "a")}})
	if !errors.Is(e, ErrCycle) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatalf("e=%v", e)
	}
	x, e := s.Apply(Batch{Ops: []Op{node(UpdateNode, "a", "z")}})
	if e != nil || x.Revision != 4 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}
func TestRemoveDeleteAndReuse(t *testing.T) {
	s := ds(t)
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "a"), node(AddNode, "b", "b"), edge(AddEdge, "a", "b")}})
	x, e := s.Apply(Batch{Ops: []Op{edge(RemoveEdge, "a", "b"), {Kind: DeleteNode, Name: "b"}, node(AddNode, "b", "new")}})
	if e != nil || x.Revision != 6 || len(s.Snapshot().Nodes) != 2 {
		t.Fatalf("x=%+v e=%v", x, e)
	}
}
func TestFinalCapacityAndRollback(t *testing.T) {
	s, _ := New(Options{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: 8, MaxPayloadBytes: 3, MaxTotalPayloadBytes: 3})
	_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, "a", "aaa")}})
	x, e := s.Apply(Batch{Ops: []Op{{Kind: DeleteNode, Name: "a"}, node(AddNode, "b", "bb")}})
	if e != nil || x.Revision != 3 {
		t.Fatal(x, e)
	}
	before := s.Snapshot()
	_, e = s.Apply(Batch{Ops: []Op{node(AddNode, "c", "cc")}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal(e)
	}
}
func TestPayloadOwnership(t *testing.T) {
	s := ds(t)
	p := []byte("abc")
	_, _ = s.Apply(Batch{Ops: []Op{{Kind: AddNode, Name: "a", Payload: p}}})
	p[0] = 'z'
	x := s.Snapshot()
	x.Nodes[0].Payload[0] = 'y'
	if string(s.Snapshot().Nodes[0].Payload) != "abc" {
		t.Fatal("alias")
	}
}
func TestConcurrentReadsWrites(t *testing.T) {
	s := ds(t)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := string(rune('a' + i))
			_, _ = s.Apply(Batch{Ops: []Op{node(AddNode, n, n)}})
			_ = s.Topological()
			_ = s.Snapshot()
		}()
	}
	wg.Wait()
	if len(s.Snapshot().Nodes) != 6 {
		t.Fatal(len(s.Snapshot().Nodes))
	}
}
