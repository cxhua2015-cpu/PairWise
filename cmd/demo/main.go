package main

import (
	"example.com/pairwise/topologygraph298/topologygraph298"
	"fmt"
)

func main() {
	g, _ := topologygraph298.New(topologygraph298.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph298.Batch{Ops: []topologygraph298.Op{{Kind: topologygraph298.AddNode, From: "a"}, {Kind: topologygraph298.AddNode, From: "b"}, {Kind: topologygraph298.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
