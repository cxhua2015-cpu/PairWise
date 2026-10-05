package main

import (
	"example.com/pairwise/topologygraph253/topologygraph253"
	"fmt"
)

func main() {
	g, _ := topologygraph253.New(topologygraph253.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph253.Batch{Ops: []topologygraph253.Op{{Kind: topologygraph253.AddNode, From: "a"}, {Kind: topologygraph253.AddNode, From: "b"}, {Kind: topologygraph253.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
