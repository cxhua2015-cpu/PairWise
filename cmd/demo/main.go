package main

import (
	"example.com/pairwise/topologygraph443/topologygraph443"
	"fmt"
)

func main() {
	g, _ := topologygraph443.New(topologygraph443.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph443.Batch{Ops: []topologygraph443.Op{{Kind: topologygraph443.AddNode, From: "a"}, {Kind: topologygraph443.AddNode, From: "b"}, {Kind: topologygraph443.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
