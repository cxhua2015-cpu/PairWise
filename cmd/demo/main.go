package main

import (
	"example.com/pairwise/artifactindex/artifactindex"
	"fmt"
)

func main() {
	s, _ := artifactindex.New(artifactindex.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(artifactindex.Batch{Ops: []artifactindex.Op{{Kind: artifactindex.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
