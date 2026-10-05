package main

import (
	"example.com/pairwise/topologygraph203/topologygraph203"
	"fmt"
)

func main() {
	g, _ := topologygraph203.New(topologygraph203.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph203.Batch{Ops: []topologygraph203.Op{{Kind: topologygraph203.AddNode, From: "a"}, {Kind: topologygraph203.AddNode, From: "b"}, {Kind: topologygraph203.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
