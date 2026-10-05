package main

import (
	"example.com/pairwise/controlgraph148/controlgraph148"
	"fmt"
)

func main() {
	g, _ := controlgraph148.New(controlgraph148.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph148.Batch{Ops: []controlgraph148.Op{{Kind: controlgraph148.AddNode, From: "a"}, {Kind: controlgraph148.AddNode, From: "b"}, {Kind: controlgraph148.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
