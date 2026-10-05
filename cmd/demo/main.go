package main

import (
	"example.com/pairwise/topologygraph248/topologygraph248"
	"fmt"
)

func main() {
	g, _ := topologygraph248.New(topologygraph248.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph248.Batch{Ops: []topologygraph248.Op{{Kind: topologygraph248.AddNode, From: "a"}, {Kind: topologygraph248.AddNode, From: "b"}, {Kind: topologygraph248.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
