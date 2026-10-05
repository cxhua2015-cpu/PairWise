package main

import (
	"example.com/pairwise/topologygraph348/topologygraph348"
	"fmt"
)

func main() {
	g, _ := topologygraph348.New(topologygraph348.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph348.Batch{Ops: []topologygraph348.Op{{Kind: topologygraph348.AddNode, From: "a"}, {Kind: topologygraph348.AddNode, From: "b"}, {Kind: topologygraph348.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
