package main

import (
	"example.com/pairwise/controlgraph088/controlgraph088"
	"fmt"
)

func main() {
	g, _ := controlgraph088.New(controlgraph088.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph088.Batch{Ops: []controlgraph088.Op{{Kind: controlgraph088.AddNode, From: "a"}, {Kind: controlgraph088.AddNode, From: "b"}, {Kind: controlgraph088.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
