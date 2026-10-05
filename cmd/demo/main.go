package main

import (
	"example.com/pairwise/replicationgraph/replicationgraph"
	"fmt"
)

func main() {
	g, _ := replicationgraph.New(replicationgraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(replicationgraph.Batch{Ops: []replicationgraph.Op{{Kind: replicationgraph.AddNode, From: "a"}, {Kind: replicationgraph.AddNode, From: "b"}, {Kind: replicationgraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
