package main

import (
	"example.com/pairwise/controlgraph108/controlgraph108"
	"fmt"
)

func main() {
	g, _ := controlgraph108.New(controlgraph108.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph108.Batch{Ops: []controlgraph108.Op{{Kind: controlgraph108.AddNode, From: "a"}, {Kind: controlgraph108.AddNode, From: "b"}, {Kind: controlgraph108.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
