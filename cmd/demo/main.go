package main

import (
	"example.com/pairwise/topologygraph343/topologygraph343"
	"fmt"
)

func main() {
	g, _ := topologygraph343.New(topologygraph343.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph343.Batch{Ops: []topologygraph343.Op{{Kind: topologygraph343.AddNode, From: "a"}, {Kind: topologygraph343.AddNode, From: "b"}, {Kind: topologygraph343.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
