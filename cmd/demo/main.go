package main

import (
	"example.com/pairwise/controlgraph168/controlgraph168"
	"fmt"
)

func main() {
	g, _ := controlgraph168.New(controlgraph168.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph168.Batch{Ops: []controlgraph168.Op{{Kind: controlgraph168.AddNode, From: "a"}, {Kind: controlgraph168.AddNode, From: "b"}, {Kind: controlgraph168.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
