package main

import (
	"example.com/pairwise/metacatalog321/metacatalog321"
	"fmt"
)

func main() {
	s, _ := metacatalog321.New(metacatalog321.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog321.Batch{Ops: []metacatalog321.Op{{Kind: metacatalog321.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
