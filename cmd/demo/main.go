package main

import (
	"example.com/pairwise/topologygraph488/topologygraph488"
	"fmt"
)

func main() {
	g, _ := topologygraph488.New(topologygraph488.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph488.Batch{Ops: []topologygraph488.Op{{Kind: topologygraph488.AddNode, From: "a"}, {Kind: topologygraph488.AddNode, From: "b"}, {Kind: topologygraph488.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
