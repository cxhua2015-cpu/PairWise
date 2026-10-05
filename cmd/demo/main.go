package main

import (
	"example.com/pairwise/dependencygraph/dependencygraph"
	"fmt"
)

func main() {
	g, _ := dependencygraph.New(dependencygraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(dependencygraph.Batch{Ops: []dependencygraph.Op{{Kind: dependencygraph.AddNode, From: "a"}, {Kind: dependencygraph.AddNode, From: "b"}, {Kind: dependencygraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
