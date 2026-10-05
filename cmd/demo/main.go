package main

import (
	"example.com/pairwise/controlgraph093/controlgraph093"
	"fmt"
)

func main() {
	g, _ := controlgraph093.New(controlgraph093.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph093.Batch{Ops: []controlgraph093.Op{{Kind: controlgraph093.AddNode, From: "a"}, {Kind: controlgraph093.AddNode, From: "b"}, {Kind: controlgraph093.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
