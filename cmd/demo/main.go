package main

import (
	"example.com/pairwise/topologygraph213/topologygraph213"
	"fmt"
)

func main() {
	g, _ := topologygraph213.New(topologygraph213.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph213.Batch{Ops: []topologygraph213.Op{{Kind: topologygraph213.AddNode, From: "a"}, {Kind: topologygraph213.AddNode, From: "b"}, {Kind: topologygraph213.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
