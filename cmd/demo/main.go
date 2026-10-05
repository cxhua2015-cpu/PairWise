package main

import (
	"example.com/pairwise/topologygraph293/topologygraph293"
	"fmt"
)

func main() {
	g, _ := topologygraph293.New(topologygraph293.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph293.Batch{Ops: []topologygraph293.Op{{Kind: topologygraph293.AddNode, From: "a"}, {Kind: topologygraph293.AddNode, From: "b"}, {Kind: topologygraph293.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
