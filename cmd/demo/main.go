package main

import (
	"example.com/pairwise/controlgraph118/controlgraph118"
	"fmt"
)

func main() {
	g, _ := controlgraph118.New(controlgraph118.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph118.Batch{Ops: []controlgraph118.Op{{Kind: controlgraph118.AddNode, From: "a"}, {Kind: controlgraph118.AddNode, From: "b"}, {Kind: controlgraph118.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
