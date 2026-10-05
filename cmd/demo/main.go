package main

import (
	"example.com/pairwise/controlgraph178/controlgraph178"
	"fmt"
)

func main() {
	g, _ := controlgraph178.New(controlgraph178.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph178.Batch{Ops: []controlgraph178.Op{{Kind: controlgraph178.AddNode, From: "a"}, {Kind: controlgraph178.AddNode, From: "b"}, {Kind: controlgraph178.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
