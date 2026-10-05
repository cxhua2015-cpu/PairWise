package main

import (
	"example.com/pairwise/topologygraph243/topologygraph243"
	"fmt"
)

func main() {
	g, _ := topologygraph243.New(topologygraph243.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph243.Batch{Ops: []topologygraph243.Op{{Kind: topologygraph243.AddNode, From: "a"}, {Kind: topologygraph243.AddNode, From: "b"}, {Kind: topologygraph243.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
