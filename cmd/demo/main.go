package main

import (
	"example.com/pairwise/topologygraph378/topologygraph378"
	"fmt"
)

func main() {
	g, _ := topologygraph378.New(topologygraph378.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph378.Batch{Ops: []topologygraph378.Op{{Kind: topologygraph378.AddNode, From: "a"}, {Kind: topologygraph378.AddNode, From: "b"}, {Kind: topologygraph378.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
