package main

import (
	"example.com/pairwise/controlgraph198/controlgraph198"
	"fmt"
)

func main() {
	g, _ := controlgraph198.New(controlgraph198.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph198.Batch{Ops: []controlgraph198.Op{{Kind: controlgraph198.AddNode, From: "a"}, {Kind: controlgraph198.AddNode, From: "b"}, {Kind: controlgraph198.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
