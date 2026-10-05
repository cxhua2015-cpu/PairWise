package main

import (
	"example.com/pairwise/controlgraph153/controlgraph153"
	"fmt"
)

func main() {
	g, _ := controlgraph153.New(controlgraph153.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph153.Batch{Ops: []controlgraph153.Op{{Kind: controlgraph153.AddNode, From: "a"}, {Kind: controlgraph153.AddNode, From: "b"}, {Kind: controlgraph153.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
