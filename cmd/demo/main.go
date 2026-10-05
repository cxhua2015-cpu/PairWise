package main

import (
	"example.com/pairwise/controlgraph173/controlgraph173"
	"fmt"
)

func main() {
	g, _ := controlgraph173.New(controlgraph173.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(controlgraph173.Batch{Ops: []controlgraph173.Op{{Kind: controlgraph173.AddNode, From: "a"}, {Kind: controlgraph173.AddNode, From: "b"}, {Kind: controlgraph173.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
