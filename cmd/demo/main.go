package main

import (
	"example.com/pairwise/rolloutgraph/rolloutgraph"
	"fmt"
)

func main() {
	g, _ := rolloutgraph.New(rolloutgraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(rolloutgraph.Batch{Ops: []rolloutgraph.Op{{Kind: rolloutgraph.AddNode, From: "a"}, {Kind: rolloutgraph.AddNode, From: "b"}, {Kind: rolloutgraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
