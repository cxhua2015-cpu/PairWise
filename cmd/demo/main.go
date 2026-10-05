package main

import (
	"example.com/pairwise/metacatalog461/metacatalog461"
	"fmt"
)

func main() {
	s, _ := metacatalog461.New(metacatalog461.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog461.Batch{Ops: []metacatalog461.Op{{Kind: metacatalog461.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
