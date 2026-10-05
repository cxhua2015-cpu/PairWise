package main

import (
	"example.com/pairwise/controlgraph183/controlgraph183"
	"fmt"
)

func main() {
	g, _ := controlgraph183.New(controlgraph183.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph183.Batch{Ops: []controlgraph183.Op{{Kind: controlgraph183.AddNode, From: "a"}, {Kind: controlgraph183.AddNode, From: "b"}, {Kind: controlgraph183.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
