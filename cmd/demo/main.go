package main

import (
	"example.com/pairwise/topologygraph478/topologygraph478"
	"fmt"
)

func main() {
	g, _ := topologygraph478.New(topologygraph478.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph478.Batch{Ops: []topologygraph478.Op{{Kind: topologygraph478.AddNode, From: "a"}, {Kind: topologygraph478.AddNode, From: "b"}, {Kind: topologygraph478.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
