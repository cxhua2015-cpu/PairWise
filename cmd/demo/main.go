package main

import (
	"example.com/pairwise/topologygraph258/topologygraph258"
	"fmt"
)

func main() {
	g, _ := topologygraph258.New(topologygraph258.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph258.Batch{Ops: []topologygraph258.Op{{Kind: topologygraph258.AddNode, From: "a"}, {Kind: topologygraph258.AddNode, From: "b"}, {Kind: topologygraph258.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
