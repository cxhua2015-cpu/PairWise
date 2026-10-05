package main

import (
	"example.com/pairwise/metacatalog381/metacatalog381"
	"fmt"
)

func main() {
	s, _ := metacatalog381.New(metacatalog381.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog381.Batch{Ops: []metacatalog381.Op{{Kind: metacatalog381.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
