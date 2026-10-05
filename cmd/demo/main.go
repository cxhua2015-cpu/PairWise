package main

import (
	"example.com/pairwise/topologygraph303/topologygraph303"
	"fmt"
)

func main() {
	g, _ := topologygraph303.New(topologygraph303.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph303.Batch{Ops: []topologygraph303.Op{{Kind: topologygraph303.AddNode, From: "a"}, {Kind: topologygraph303.AddNode, From: "b"}, {Kind: topologygraph303.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
