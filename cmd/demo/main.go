package main

import (
	"example.com/pairwise/topologygraph483/topologygraph483"
	"fmt"
)

func main() {
	g, _ := topologygraph483.New(topologygraph483.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph483.Batch{Ops: []topologygraph483.Op{{Kind: topologygraph483.AddNode, From: "a"}, {Kind: topologygraph483.AddNode, From: "b"}, {Kind: topologygraph483.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
