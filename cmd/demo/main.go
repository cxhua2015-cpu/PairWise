package main

import (
	"example.com/pairwise/topologygraph468/topologygraph468"
	"fmt"
)

func main() {
	g, _ := topologygraph468.New(topologygraph468.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph468.Batch{Ops: []topologygraph468.Op{{Kind: topologygraph468.AddNode, From: "a"}, {Kind: topologygraph468.AddNode, From: "b"}, {Kind: topologygraph468.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
