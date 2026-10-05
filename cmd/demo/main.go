package main

import (
	"example.com/pairwise/topologygraph453/topologygraph453"
	"fmt"
)

func main() {
	g, _ := topologygraph453.New(topologygraph453.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph453.Batch{Ops: []topologygraph453.Op{{Kind: topologygraph453.AddNode, From: "a"}, {Kind: topologygraph453.AddNode, From: "b"}, {Kind: topologygraph453.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
