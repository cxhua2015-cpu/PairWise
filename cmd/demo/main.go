package main

import (
	"example.com/pairwise/topologygraph323/topologygraph323"
	"fmt"
)

func main() {
	g, _ := topologygraph323.New(topologygraph323.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph323.Batch{Ops: []topologygraph323.Op{{Kind: topologygraph323.AddNode, From: "a"}, {Kind: topologygraph323.AddNode, From: "b"}, {Kind: topologygraph323.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
