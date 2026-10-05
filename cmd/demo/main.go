package main

import (
	"example.com/pairwise/topologygraph338/topologygraph338"
	"fmt"
)

func main() {
	g, _ := topologygraph338.New(topologygraph338.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph338.Batch{Ops: []topologygraph338.Op{{Kind: topologygraph338.AddNode, From: "a"}, {Kind: topologygraph338.AddNode, From: "b"}, {Kind: topologygraph338.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
