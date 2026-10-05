package pipelinegraph

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxNodes: 0, MaxEdges: 1, MaxNameBytes: 1},
		{MaxNodes: 1, MaxEdges: 0, MaxNameBytes: 1},
		{MaxNodes: 1, MaxEdges: 1, MaxNameBytes: 0},
		{MaxNodes: -1, MaxEdges: 1, MaxNameBytes: 1},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: want ErrInvalidOptions, got %v", o, err)
		}
	}
}

func TestNameValidation(t *testing.T) {
	g := graph(t)
	bad := []string{"", "A", "a b", "a.b", "a/b", "é", "toolongname"}
	for _, name := range bad {
		_, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: name}}})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("name %q: want ErrInvalidInput, got %v", name, err)
		}
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "ok-n_1"}}}); err != nil {
		t.Fatal(err)
	}
	// Node ops must not carry To.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "x", To: "y"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("expected ErrInvalidInput for node op with To")
	}
	// Unknown kind.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: Kind(0), From: "x"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("expected ErrInvalidInput for unknown kind")
	}
	// Name length limit applies to Reachable too.
	if _, err := g.Reachable("ok-n_1", "waytoolongname"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("expected ErrInvalidInput from Reachable")
	}
}

func TestValidationBeforeState(t *testing.T) {
	g := graph(t)
	// Structurally invalid op later in the batch must fail the whole batch
	// without touching state, even though earlier ops are valid.
	_, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: Kind(99), From: "b"}}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); got.Generation != 0 || len(got.Nodes) != 0 {
		t.Fatalf("state changed: %+v", got)
	}
}

func TestEmptyBatchKeepsGeneration(t *testing.T) {
	g := graph(t)
	r0, err := g.Apply(Batch{})
	if err != nil || r0.Generation != 0 {
		t.Fatalf("empty batch: %+v %v", r0, err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); err != nil {
		t.Fatal(err)
	}
	r1, err := g.Apply(Batch{})
	if err != nil || r1.Generation != 1 {
		t.Fatalf("empty batch after write: %+v %v", r1, err)
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	g := graph(t)
	r, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"},
		{Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "a", To: "b"},
	}})
	if err != nil || r.Generation != 1 {
		t.Fatalf("got %+v %v", r, err)
	}
	if got := g.Snapshot().Generation; got != 1 {
		t.Fatal(got)
	}
	// Failed batch must not bump generation.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if got := g.Snapshot().Generation; got != 1 {
		t.Fatal(got)
	}
}

func TestRollbackOnFailure(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}}}); err != nil {
		t.Fatal(err)
	}
	before := g.Snapshot()
	// Duplicate edge at the end invalidates the whole batch.
	_, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"},
		{Kind: AddEdge, From: "a", To: "b"},
		{Kind: AddEdge, From: "a", To: "b"},
	}})
	if !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, g.Snapshot()) {
		t.Fatal("state changed after failed batch")
	}
}

func TestCapacityRollback(t *testing.T) {
	g, err := New(Options{MaxNodes: 2, MaxEdges: 1, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}}}); err != nil {
		t.Fatal(err)
	}
	// Node capacity exceeded only at batch end.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "c"}}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Edge capacity exceeded only at batch end (also a cycle; either error
	// is acceptable as long as the batch fails and rolls back).
	if _, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddEdge, From: "a", To: "b"},
		{Kind: AddEdge, From: "b", To: "a"},
	}}); !errors.Is(err, ErrCycle) && !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	if got := g.Snapshot(); len(got.Nodes) != 2 || len(got.Edges) != 0 || got.Generation != 1 {
		t.Fatalf("state changed: %+v", got)
	}
	// Delete-then-add within one batch stays within capacity.
	if _, err := g.Apply(Batch{Ops: []Op{
		{Kind: DeleteNode, From: "a"},
		{Kind: AddNode, From: "c"},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddNode, From: "c"},
		{Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "b", To: "c"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "b"}}}); err != nil {
		t.Fatal(err)
	}
	if snap := g.Snapshot(); len(snap.Edges) != 0 {
		t.Fatalf("edges remain: %+v", snap.Edges)
	}
	// Deleting the middle node must break reachability.
	if ok, err := g.Reachable("a", "c"); err != nil || ok {
		t.Fatal("a should not reach c after b deleted")
	}
}

func TestNotFoundAndExists(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: "ghost"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: DeleteEdge, From: "a", To: "b"}}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := g.Reachable("a", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSelfLoopCycle(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "a", To: "a"}}}); !errors.Is(err, ErrCycle) {
		t.Fatal(err)
	}
}

func TestSnapshotOrderingAndIsolation(t *testing.T) {
	g := graph(t)
	if _, err := g.Apply(Batch{Ops: []Op{
		{Kind: AddNode, From: "c"}, {Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"},
		{Kind: AddEdge, From: "c", To: "a"}, {Kind: AddEdge, From: "a", To: "b"}, {Kind: AddEdge, From: "c", To: "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	snap := g.Snapshot()
	wantNodes := []string{"a", "b", "c"}
	wantEdges := []Edge{{"a", "b"}, {"c", "a"}, {"c", "b"}}
	if !reflect.DeepEqual(snap.Nodes, wantNodes) || !reflect.DeepEqual(snap.Edges, wantEdges) {
		t.Fatalf("bad snapshot: %+v", snap)
	}
	// Mutating returned slices must not affect internal state.
	snap.Nodes[0] = "zzz"
	snap.Edges[0].From = "zzz"
	again := g.Snapshot()
	if !reflect.DeepEqual(again.Nodes, wantNodes) || !reflect.DeepEqual(again.Edges, wantEdges) {
		t.Fatal("snapshot shares memory with internal state")
	}
}

func TestConcurrentMixed(t *testing.T) {
	g, err := New(Options{MaxNodes: 128, MaxEdges: 256, MaxNameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("n%02d", i)
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: name}}})
			_, _ = g.Reachable(name, name)
			_ = g.Snapshot()
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: DeleteNode, From: name}}})
		}()
	}
	// Concurrent edge writers on a shared pair of nodes.
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "src"}, {Kind: AddNode, From: "dst"}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = g.Apply(Batch{Ops: []Op{{Kind: AddEdge, From: "src", To: "dst"}}})
			_, _ = g.Reachable("src", "dst")
		}()
	}
	wg.Wait()
	snap := g.Snapshot()
	if len(snap.Edges) > 1 {
		t.Fatalf("duplicate edges committed: %+v", snap.Edges)
	}
}
