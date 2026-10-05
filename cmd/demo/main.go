package main

import (
	"example.com/pairwise/topologygraph228/topologygraph228"
	"fmt"
)

func main() {
	g, _ := topologygraph228.New(topologygraph228.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph228.Batch{Ops: []topologygraph228.Op{{Kind: topologygraph228.AddNode, From: "a"}, {Kind: topologygraph228.AddNode, From: "b"}, {Kind: topologygraph228.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
