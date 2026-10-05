package main

import (
	"example.com/pairwise/topologygraph433/topologygraph433"
	"fmt"
)

func main() {
	g, _ := topologygraph433.New(topologygraph433.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph433.Batch{Ops: []topologygraph433.Op{{Kind: topologygraph433.AddNode, From: "a"}, {Kind: topologygraph433.AddNode, From: "b"}, {Kind: topologygraph433.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
