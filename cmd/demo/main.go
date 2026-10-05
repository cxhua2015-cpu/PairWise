package main

import (
	"example.com/pairwise/topologygraph373/topologygraph373"
	"fmt"
)

func main() {
	g, _ := topologygraph373.New(topologygraph373.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph373.Batch{Ops: []topologygraph373.Op{{Kind: topologygraph373.AddNode, From: "a"}, {Kind: topologygraph373.AddNode, From: "b"}, {Kind: topologygraph373.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
