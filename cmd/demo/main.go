package main

import (
	"example.com/pairwise/topologygraph393/topologygraph393"
	"fmt"
)

func main() {
	g, _ := topologygraph393.New(topologygraph393.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph393.Batch{Ops: []topologygraph393.Op{{Kind: topologygraph393.AddNode, From: "a"}, {Kind: topologygraph393.AddNode, From: "b"}, {Kind: topologygraph393.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
