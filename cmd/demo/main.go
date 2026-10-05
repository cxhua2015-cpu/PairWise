package main

import (
	"example.com/pairwise/controlgraph138/controlgraph138"
	"fmt"
)

func main() {
	g, _ := controlgraph138.New(controlgraph138.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph138.Batch{Ops: []controlgraph138.Op{{Kind: controlgraph138.AddNode, From: "a"}, {Kind: controlgraph138.AddNode, From: "b"}, {Kind: controlgraph138.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
