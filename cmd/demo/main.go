package main

import (
	"example.com/pairwise/topologygraph438/topologygraph438"
	"fmt"
)

func main() {
	g, _ := topologygraph438.New(topologygraph438.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph438.Batch{Ops: []topologygraph438.Op{{Kind: topologygraph438.AddNode, From: "a"}, {Kind: topologygraph438.AddNode, From: "b"}, {Kind: topologygraph438.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
