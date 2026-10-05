package main

import (
	"example.com/pairwise/topologygraph313/topologygraph313"
	"fmt"
)

func main() {
	g, _ := topologygraph313.New(topologygraph313.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph313.Batch{Ops: []topologygraph313.Op{{Kind: topologygraph313.AddNode, From: "a"}, {Kind: topologygraph313.AddNode, From: "b"}, {Kind: topologygraph313.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
