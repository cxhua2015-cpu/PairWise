package main

import (
	"example.com/pairwise/topologygraph233/topologygraph233"
	"fmt"
)

func main() {
	g, _ := topologygraph233.New(topologygraph233.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph233.Batch{Ops: []topologygraph233.Op{{Kind: topologygraph233.AddNode, From: "a"}, {Kind: topologygraph233.AddNode, From: "b"}, {Kind: topologygraph233.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
