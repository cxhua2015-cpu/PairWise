package main

import (
	"example.com/pairwise/servicegraph/servicegraph"
	"fmt"
)

func main() {
	g, _ := servicegraph.New(servicegraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(servicegraph.Batch{Ops: []servicegraph.Op{{Kind: servicegraph.AddNode, From: "a"}, {Kind: servicegraph.AddNode, From: "b"}, {Kind: servicegraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
