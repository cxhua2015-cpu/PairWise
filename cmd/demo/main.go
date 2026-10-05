package main

import (
	"example.com/pairwise/topologygraph388/topologygraph388"
	"fmt"
)

func main() {
	g, _ := topologygraph388.New(topologygraph388.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph388.Batch{Ops: []topologygraph388.Op{{Kind: topologygraph388.AddNode, From: "a"}, {Kind: topologygraph388.AddNode, From: "b"}, {Kind: topologygraph388.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
