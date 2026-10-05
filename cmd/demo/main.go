package main

import (
	"example.com/pairwise/topologygraph408/topologygraph408"
	"fmt"
)

func main() {
	g, _ := topologygraph408.New(topologygraph408.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph408.Batch{Ops: []topologygraph408.Op{{Kind: topologygraph408.AddNode, From: "a"}, {Kind: topologygraph408.AddNode, From: "b"}, {Kind: topologygraph408.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
