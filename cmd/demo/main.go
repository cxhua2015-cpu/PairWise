package main

import (
	"example.com/pairwise/topologygraph353/topologygraph353"
	"fmt"
)

func main() {
	g, _ := topologygraph353.New(topologygraph353.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph353.Batch{Ops: []topologygraph353.Op{{Kind: topologygraph353.AddNode, From: "a"}, {Kind: topologygraph353.AddNode, From: "b"}, {Kind: topologygraph353.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
