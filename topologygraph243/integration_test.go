package topologygraph243

import "testing"

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
