package main

import (
	"example.com/pairwise/topologygraph358/topologygraph358"
	"fmt"
)

func main() {
	g, _ := topologygraph358.New(topologygraph358.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph358.Batch{Ops: []topologygraph358.Op{{Kind: topologygraph358.AddNode, From: "a"}, {Kind: topologygraph358.AddNode, From: "b"}, {Kind: topologygraph358.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
