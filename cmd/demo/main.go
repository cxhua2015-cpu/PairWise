package main

import (
	"example.com/pairwise/topologygraph473/topologygraph473"
	"fmt"
)

func main() {
	g, _ := topologygraph473.New(topologygraph473.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph473.Batch{Ops: []topologygraph473.Op{{Kind: topologygraph473.AddNode, From: "a"}, {Kind: topologygraph473.AddNode, From: "b"}, {Kind: topologygraph473.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
