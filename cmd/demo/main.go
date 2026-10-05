package main

import (
	"example.com/pairwise/topologygraph238/topologygraph238"
	"fmt"
)

func main() {
	g, _ := topologygraph238.New(topologygraph238.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph238.Batch{Ops: []topologygraph238.Op{{Kind: topologygraph238.AddNode, From: "a"}, {Kind: topologygraph238.AddNode, From: "b"}, {Kind: topologygraph238.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
