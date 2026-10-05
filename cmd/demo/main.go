package main

import (
	"example.com/pairwise/topologygraph333/topologygraph333"
	"fmt"
)

func main() {
	g, _ := topologygraph333.New(topologygraph333.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph333.Batch{Ops: []topologygraph333.Op{{Kind: topologygraph333.AddNode, From: "a"}, {Kind: topologygraph333.AddNode, From: "b"}, {Kind: topologygraph333.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
