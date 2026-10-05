package main

import (
	"example.com/pairwise/controlgraph158/controlgraph158"
	"fmt"
)

func main() {
	g, _ := controlgraph158.New(controlgraph158.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph158.Batch{Ops: []controlgraph158.Op{{Kind: controlgraph158.AddNode, From: "a"}, {Kind: controlgraph158.AddNode, From: "b"}, {Kind: controlgraph158.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
