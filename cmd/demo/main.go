package main

import (
	"example.com/pairwise/topologygraph418/topologygraph418"
	"fmt"
)

func main() {
	g, _ := topologygraph418.New(topologygraph418.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph418.Batch{Ops: []topologygraph418.Op{{Kind: topologygraph418.AddNode, From: "a"}, {Kind: topologygraph418.AddNode, From: "b"}, {Kind: topologygraph418.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
