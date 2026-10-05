package main

import (
	"example.com/pairwise/topologygraph368/topologygraph368"
	"fmt"
)

func main() {
	g, _ := topologygraph368.New(topologygraph368.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph368.Batch{Ops: []topologygraph368.Op{{Kind: topologygraph368.AddNode, From: "a"}, {Kind: topologygraph368.AddNode, From: "b"}, {Kind: topologygraph368.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
