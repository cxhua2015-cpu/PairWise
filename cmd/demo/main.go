package main

import (
	"example.com/pairwise/dagstore/dagstore"
	"fmt"
)

func main() {
	s, _ := dagstore.New(dagstore.Options{MaxNodes: 8, MaxEdges: 8, MaxNameBytes: 16, MaxPayloadBytes: 8, MaxTotalPayloadBytes: 32})
	x, _ := s.Apply(dagstore.Batch{Ops: []dagstore.Op{{Kind: dagstore.AddNode, Name: "api", Payload: []byte("a")}, {Kind: dagstore.AddNode, Name: "db", Payload: []byte("d")}, {Kind: dagstore.AddEdge, From: "api", To: "db"}}})
	ok, _ := s.Reachable("api", "db")
	fmt.Printf("generation=%d revision=%d nodes=%d edges=%d reachable=%t topo=%v\n", x.Generation, x.Revision, len(s.Snapshot().Nodes), len(s.Snapshot().Edges), ok, s.Topological())
}
