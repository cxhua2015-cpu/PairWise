package main

import (
	"example.com/pairwise/topologygraph208/topologygraph208"
	"fmt"
)

func main() {
	g, _ := topologygraph208.New(topologygraph208.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph208.Batch{Ops: []topologygraph208.Op{{Kind: topologygraph208.AddNode, From: "a"}, {Kind: topologygraph208.AddNode, From: "b"}, {Kind: topologygraph208.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
