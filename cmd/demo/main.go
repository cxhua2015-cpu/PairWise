package main

import (
	"example.com/pairwise/topologygraph318/topologygraph318"
	"fmt"
)

func main() {
	g, _ := topologygraph318.New(topologygraph318.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph318.Batch{Ops: []topologygraph318.Op{{Kind: topologygraph318.AddNode, From: "a"}, {Kind: topologygraph318.AddNode, From: "b"}, {Kind: topologygraph318.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
