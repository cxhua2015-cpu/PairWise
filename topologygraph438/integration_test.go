package topologygraph438

import (
	"reflect"
	"testing"
)

func TestMultiFileIntegration(t *testing.T) {
	g, _ := New(Options{4, 4, 12})
	if err := g.ValidateBatch(Batch{Ops: []Op{{AddEdge, "a", "a"}}}); err != ErrInvalidInput {
		t.Fatalf("validation: %v", err)
	}
	if _, err := g.Apply(Batch{Ops: []Op{{AddNode, "a", ""}, {AddNode, "b", ""}, {AddEdge, "a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	z := g.Stats()
	if z.Nodes != 2 || z.Edges != 1 {
		t.Fatalf("stats: %+v", z)
	}
	c, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{DeleteNode, "b", ""}}})
	if len(g.Snapshot().Nodes) != 2 || len(c.Snapshot().Nodes) != 1 {
		t.Fatal("clone aliases original")
	}
}

func TestPreviewParityAndIsolation(t *testing.T) {
	g, _ := New(Options{MaxNodes: 5, MaxEdges: 5, MaxNameBytes: 12})
	if _, err := g.Apply(Batch{Ops: []Op{{Kind: AddNode, From: "a"}, {Kind: AddNode, From: "b"}, {Kind: AddEdge, From: "a", To: "b"}}}); err != nil {
		t.Fatal(err)
	}
	batch := Batch{Ops: []Op{{Kind: DeleteEdge, From: "a", To: "b"}, {Kind: AddNode, From: "c"}, {Kind: AddEdge, From: "b", To: "c"}}}

	beforeSnapshot := g.Snapshot()
	beforeStats := g.Stats()
	candidate, err := g.Clone()
	if err != nil {
		t.Fatal(err)
	}
	wantResult, err := candidate.Apply(batch)
	if err != nil {
		t.Fatal(err)
	}

	gotResult, gotSnapshot, gotStats, err := g.Preview(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotResult, wantResult) {
		t.Fatalf("result mismatch: got=%+v want=%+v", gotResult, wantResult)
	}
	if !reflect.DeepEqual(gotSnapshot, candidate.Snapshot()) {
		t.Fatalf("snapshot mismatch: got=%+v want=%+v", gotSnapshot, candidate.Snapshot())
	}
	if !reflect.DeepEqual(gotStats, candidate.Stats()) {
		t.Fatalf("stats mismatch: got=%+v want=%+v", gotStats, candidate.Stats())
	}
	if !reflect.DeepEqual(g.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(g.Stats(), beforeStats) {
		t.Fatal("preview mutated receiver")
	}

	zeroResult, zeroSnapshot, zeroStats, err := g.Preview(Batch{Ops: []Op{{Kind: DeleteNode, From: "missing"}}})
	if err != ErrNotFound {
		t.Fatalf("preview error: got=%v want=%v", err, ErrNotFound)
	}
	if !reflect.DeepEqual(zeroResult, Result{}) || !reflect.DeepEqual(zeroSnapshot, Snapshot{}) || !reflect.DeepEqual(zeroStats, Stats{}) {
		t.Fatal("failed preview returned non-zero values")
	}
	if !reflect.DeepEqual(g.Snapshot(), beforeSnapshot) || !reflect.DeepEqual(g.Stats(), beforeStats) {
		t.Fatal("failed preview mutated receiver")
	}
}
