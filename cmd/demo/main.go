package main

import (
	"example.com/pairwise/metacatalog206/metacatalog206"
	"fmt"
)

func main() {
	s, _ := metacatalog206.New(metacatalog206.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog206.Batch{Ops: []metacatalog206.Op{{Kind: metacatalog206.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
