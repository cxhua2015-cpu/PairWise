package main

import (
	"example.com/pairwise/topologygraph273/topologygraph273"
	"fmt"
)

func main() {
	g, _ := topologygraph273.New(topologygraph273.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph273.Batch{Ops: []topologygraph273.Op{{Kind: topologygraph273.AddNode, From: "a"}, {Kind: topologygraph273.AddNode, From: "b"}, {Kind: topologygraph273.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
