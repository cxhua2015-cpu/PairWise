package main

import (
	"example.com/pairwise/topologygraph403/topologygraph403"
	"fmt"
)

func main() {
	g, _ := topologygraph403.New(topologygraph403.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph403.Batch{Ops: []topologygraph403.Op{{Kind: topologygraph403.AddNode, From: "a"}, {Kind: topologygraph403.AddNode, From: "b"}, {Kind: topologygraph403.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
