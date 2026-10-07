package main

import (
	"example.com/pairwise/topologygraph413/topologygraph413"
	"fmt"
)

func main() {
	g, _ := topologygraph413.New(topologygraph413.Options{MaxNodes: 4, MaxEdges: 4, MaxNameBytes: 8})
	x, _ := g.Apply(topologygraph413.Batch{Ops: []topologygraph413.Op{{Kind: topologygraph413.AddNode, From: "a"}, {Kind: topologygraph413.AddNode, From: "b"}, {Kind: topologygraph413.AddEdge, From: "a", To: "b"}}})
	ok, _ := g.Reachable("a", "b")
	fmt.Printf("generation=%d nodes=%d edges=%d reachable=%t\n", x.Generation, len(g.Snapshot().Nodes), len(g.Snapshot().Edges), ok)
}
