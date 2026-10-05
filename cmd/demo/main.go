package main

import (
	"example.com/pairwise/controlgraph103/controlgraph103"
	"fmt"
)

func main() {
	g, _ := controlgraph103.New(controlgraph103.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph103.Batch{Ops: []controlgraph103.Op{{Kind: controlgraph103.AddNode, From: "a"}, {Kind: controlgraph103.AddNode, From: "b"}, {Kind: controlgraph103.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
