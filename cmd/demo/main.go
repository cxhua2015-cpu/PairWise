package main

import (
	"example.com/pairwise/metacatalog446/metacatalog446"
	"fmt"
)

func main() {
	s, _ := metacatalog446.New(metacatalog446.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog446.Batch{Ops: []metacatalog446.Op{{Kind: metacatalog446.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
