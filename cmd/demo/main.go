package main

import (
	"example.com/pairwise/topologygraph498/topologygraph498"
	"fmt"
)

func main() {
	g, _ := topologygraph498.New(topologygraph498.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph498.Batch{Ops: []topologygraph498.Op{{Kind: topologygraph498.AddNode, From: "a"}, {Kind: topologygraph498.AddNode, From: "b"}, {Kind: topologygraph498.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
