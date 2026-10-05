package main

import (
	"example.com/pairwise/topologygraph263/topologygraph263"
	"fmt"
)

func main() {
	g, _ := topologygraph263.New(topologygraph263.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph263.Batch{Ops: []topologygraph263.Op{{Kind: topologygraph263.AddNode, From: "a"}, {Kind: topologygraph263.AddNode, From: "b"}, {Kind: topologygraph263.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
