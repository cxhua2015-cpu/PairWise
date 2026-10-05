package main

import (
	"example.com/pairwise/controlgraph098/controlgraph098"
	"fmt"
)

func main() {
	g, _ := controlgraph098.New(controlgraph098.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph098.Batch{Ops: []controlgraph098.Op{{Kind: controlgraph098.AddNode, From: "a"}, {Kind: controlgraph098.AddNode, From: "b"}, {Kind: controlgraph098.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
