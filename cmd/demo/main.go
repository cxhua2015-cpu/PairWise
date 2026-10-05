package main

import (
	"example.com/pairwise/topologygraph493/topologygraph493"
	"fmt"
)

func main() {
	g, _ := topologygraph493.New(topologygraph493.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph493.Batch{Ops: []topologygraph493.Op{{Kind: topologygraph493.AddNode, From: "a"}, {Kind: topologygraph493.AddNode, From: "b"}, {Kind: topologygraph493.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
