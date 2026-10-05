package main

import (
	"example.com/pairwise/failovergraph/failovergraph"
	"fmt"
)

func main() {
	g, _ := failovergraph.New(failovergraph.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(failovergraph.Batch{Ops: []failovergraph.Op{{Kind: failovergraph.AddNode, From: "a"}, {Kind: failovergraph.AddNode, From: "b"}, {Kind: failovergraph.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
