package main

import (
	"example.com/pairwise/topologygraph423/topologygraph423"
	"fmt"
)

func main() {
	g, _ := topologygraph423.New(topologygraph423.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph423.Batch{Ops: []topologygraph423.Op{{Kind: topologygraph423.AddNode, From: "a"}, {Kind: topologygraph423.AddNode, From: "b"}, {Kind: topologygraph423.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
