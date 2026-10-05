package main

import (
	"example.com/pairwise/topologygraph328/topologygraph328"
	"fmt"
)

func main() {
	g, _ := topologygraph328.New(topologygraph328.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph328.Batch{Ops: []topologygraph328.Op{{Kind: topologygraph328.AddNode, From: "a"}, {Kind: topologygraph328.AddNode, From: "b"}, {Kind: topologygraph328.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
