package main

import (
	"example.com/pairwise/trustgraph/trustgraph"
	"fmt"
)

func main() {
	g, _ := trustgraph.New(trustgraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(trustgraph.Batch{Ops: []trustgraph.Op{{Kind: trustgraph.AddNode, From: "a"}, {Kind: trustgraph.AddNode, From: "b"}, {Kind: trustgraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
