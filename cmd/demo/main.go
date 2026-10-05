package main

import (
	"example.com/pairwise/controlgraph193/controlgraph193"
	"fmt"
)

func main() {
	g, _ := controlgraph193.New(controlgraph193.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph193.Batch{Ops: []controlgraph193.Op{{Kind: controlgraph193.AddNode, From: "a"}, {Kind: controlgraph193.AddNode, From: "b"}, {Kind: controlgraph193.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
