package main

import (
	"example.com/pairwise/topologygraph268/topologygraph268"
	"fmt"
)

func main() {
	g, _ := topologygraph268.New(topologygraph268.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph268.Batch{Ops: []topologygraph268.Op{{Kind: topologygraph268.AddNode, From: "a"}, {Kind: topologygraph268.AddNode, From: "b"}, {Kind: topologygraph268.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
