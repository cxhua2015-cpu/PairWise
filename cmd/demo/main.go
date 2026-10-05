package main

import (
	"example.com/pairwise/workflowgraph/workflowgraph"
	"fmt"
)

func main() {
	g, _ := workflowgraph.New(workflowgraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(workflowgraph.Batch{Ops: []workflowgraph.Op{{Kind: workflowgraph.AddNode, From: "a"}, {Kind: workflowgraph.AddNode, From: "b"}, {Kind: workflowgraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
