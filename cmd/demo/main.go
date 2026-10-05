package main

import (
	"example.com/pairwise/topologygraph448/topologygraph448"
	"fmt"
)

func main() {
	g, _ := topologygraph448.New(topologygraph448.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph448.Batch{Ops: []topologygraph448.Op{{Kind: topologygraph448.AddNode, From: "a"}, {Kind: topologygraph448.AddNode, From: "b"}, {Kind: topologygraph448.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
