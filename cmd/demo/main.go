package main

import (
	"example.com/pairwise/controlgraph083/controlgraph083"
	"fmt"
)

func main() {
	g, _ := controlgraph083.New(controlgraph083.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph083.Batch{Ops: []controlgraph083.Op{{Kind: controlgraph083.AddNode, From: "a"}, {Kind: controlgraph083.AddNode, From: "b"}, {Kind: controlgraph083.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
