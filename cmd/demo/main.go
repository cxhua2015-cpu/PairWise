package main

import (
	"example.com/pairwise/topologygraph283/topologygraph283"
	"fmt"
)

func main() {
	g, _ := topologygraph283.New(topologygraph283.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph283.Batch{Ops: []topologygraph283.Op{{Kind: topologygraph283.AddNode, From: "a"}, {Kind: topologygraph283.AddNode, From: "b"}, {Kind: topologygraph283.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
