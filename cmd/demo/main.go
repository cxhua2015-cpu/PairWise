package main

import (
	"example.com/pairwise/topologygraph458/topologygraph458"
	"fmt"
)

func main() {
	g, _ := topologygraph458.New(topologygraph458.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph458.Batch{Ops: []topologygraph458.Op{{Kind: topologygraph458.AddNode, From: "a"}, {Kind: topologygraph458.AddNode, From: "b"}, {Kind: topologygraph458.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
