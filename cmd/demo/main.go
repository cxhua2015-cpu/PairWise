package main

import (
	"example.com/pairwise/controlgraph133/controlgraph133"
	"fmt"
)

func main() {
	g, _ := controlgraph133.New(controlgraph133.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph133.Batch{Ops: []controlgraph133.Op{{Kind: controlgraph133.AddNode, From: "a"}, {Kind: controlgraph133.AddNode, From: "b"}, {Kind: controlgraph133.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
