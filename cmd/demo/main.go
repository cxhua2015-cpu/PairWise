package main

import (
	"example.com/pairwise/controlgraph123/controlgraph123"
	"fmt"
)

func main() {
	g, _ := controlgraph123.New(controlgraph123.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph123.Batch{Ops: []controlgraph123.Op{{Kind: controlgraph123.AddNode, From: "a"}, {Kind: controlgraph123.AddNode, From: "b"}, {Kind: controlgraph123.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
