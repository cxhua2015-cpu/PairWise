package main

import (
	"example.com/pairwise/subscriptiongraph/subscriptiongraph"
	"fmt"
)

func main() {
	g, _ := subscriptiongraph.New(subscriptiongraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(subscriptiongraph.Batch{Ops: []subscriptiongraph.Op{{Kind: subscriptiongraph.AddNode, From: "a"}, {Kind: subscriptiongraph.AddNode, From: "b"}, {Kind: subscriptiongraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
