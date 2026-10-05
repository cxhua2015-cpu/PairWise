package main

import (
	"example.com/pairwise/schemaindex/schemaindex"
	"fmt"
)

func main() {
	s, _ := schemaindex.New(schemaindex.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(schemaindex.Batch{Ops: []schemaindex.Op{{Kind: schemaindex.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
