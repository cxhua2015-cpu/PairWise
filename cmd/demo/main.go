package main

import (
	"example.com/pairwise/controlgraph163/controlgraph163"
	"fmt"
)

func main() {
	g, _ := controlgraph163.New(controlgraph163.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph163.Batch{Ops: []controlgraph163.Op{{Kind: controlgraph163.AddNode, From: "a"}, {Kind: controlgraph163.AddNode, From: "b"}, {Kind: controlgraph163.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
