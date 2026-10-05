package main

import (
	"example.com/pairwise/controlgraph113/controlgraph113"
	"fmt"
)

func main() {
	g, _ := controlgraph113.New(controlgraph113.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph113.Batch{Ops: []controlgraph113.Op{{Kind: controlgraph113.AddNode, From: "a"}, {Kind: controlgraph113.AddNode, From: "b"}, {Kind: controlgraph113.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
