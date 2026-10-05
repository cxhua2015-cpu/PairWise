package main

import (
	"example.com/pairwise/topologygraph308/topologygraph308"
	"fmt"
)

func main() {
	g, _ := topologygraph308.New(topologygraph308.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph308.Batch{Ops: []topologygraph308.Op{{Kind: topologygraph308.AddNode, From: "a"}, {Kind: topologygraph308.AddNode, From: "b"}, {Kind: topologygraph308.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
