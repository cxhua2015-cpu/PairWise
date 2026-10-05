package main

import (
	"example.com/pairwise/topologygraph363/topologygraph363"
	"fmt"
)

func main() {
	g, _ := topologygraph363.New(topologygraph363.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph363.Batch{Ops: []topologygraph363.Op{{Kind: topologygraph363.AddNode, From: "a"}, {Kind: topologygraph363.AddNode, From: "b"}, {Kind: topologygraph363.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
