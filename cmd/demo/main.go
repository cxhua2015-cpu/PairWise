package main

import (
	"example.com/pairwise/ownershipgraph/ownershipgraph"
	"fmt"
)

func main() {
	g, _ := ownershipgraph.New(ownershipgraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(ownershipgraph.Batch{Ops: []ownershipgraph.Op{{Kind: ownershipgraph.AddNode, From: "a"}, {Kind: ownershipgraph.AddNode, From: "b"}, {Kind: ownershipgraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
