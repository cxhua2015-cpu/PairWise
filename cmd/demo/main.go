package main

import (
	"example.com/pairwise/pipelinegraph/pipelinegraph"
	"fmt"
)

func main() {
	g, _ := pipelinegraph.New(pipelinegraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(pipelinegraph.Batch{Ops: []pipelinegraph.Op{{Kind: pipelinegraph.AddNode, From: "a"}, {Kind: pipelinegraph.AddNode, From: "b"}, {Kind: pipelinegraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
