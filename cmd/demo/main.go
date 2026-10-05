package main

import (
	"example.com/pairwise/topologygraph463/topologygraph463"
	"fmt"
)

func main() {
	g, _ := topologygraph463.New(topologygraph463.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph463.Batch{Ops: []topologygraph463.Op{{Kind: topologygraph463.AddNode, From: "a"}, {Kind: topologygraph463.AddNode, From: "b"}, {Kind: topologygraph463.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
