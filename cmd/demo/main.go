package main

import (
	"example.com/pairwise/topologygraph428/topologygraph428"
	"fmt"
)

func main() {
	g, _ := topologygraph428.New(topologygraph428.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph428.Batch{Ops: []topologygraph428.Op{{Kind: topologygraph428.AddNode, From: "a"}, {Kind: topologygraph428.AddNode, From: "b"}, {Kind: topologygraph428.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
