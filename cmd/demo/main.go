package main

import (
	"example.com/pairwise/topologygraph383/topologygraph383"
	"fmt"
)

func main() {
	g, _ := topologygraph383.New(topologygraph383.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph383.Batch{Ops: []topologygraph383.Op{{Kind: topologygraph383.AddNode, From: "a"}, {Kind: topologygraph383.AddNode, From: "b"}, {Kind: topologygraph383.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
