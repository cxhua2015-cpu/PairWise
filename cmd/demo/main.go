package main

import (
	"example.com/pairwise/controlgraph128/controlgraph128"
	"fmt"
)

func main() {
	g, _ := controlgraph128.New(controlgraph128.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph128.Batch{Ops: []controlgraph128.Op{{Kind: controlgraph128.AddNode, From: "a"}, {Kind: controlgraph128.AddNode, From: "b"}, {Kind: controlgraph128.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
