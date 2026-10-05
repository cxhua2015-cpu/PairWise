package main

import (
	"example.com/pairwise/topologygraph218/topologygraph218"
	"fmt"
)

func main() {
	g, _ := topologygraph218.New(topologygraph218.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph218.Batch{Ops: []topologygraph218.Op{{Kind: topologygraph218.AddNode, From: "a"}, {Kind: topologygraph218.AddNode, From: "b"}, {Kind: topologygraph218.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
