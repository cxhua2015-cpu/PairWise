package main

import (
	"example.com/pairwise/topologygraph398/topologygraph398"
	"fmt"
)

func main() {
	g, _ := topologygraph398.New(topologygraph398.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph398.Batch{Ops: []topologygraph398.Op{{Kind: topologygraph398.AddNode, From: "a"}, {Kind: topologygraph398.AddNode, From: "b"}, {Kind: topologygraph398.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
