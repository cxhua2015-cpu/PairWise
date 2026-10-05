package main

import (
	"example.com/pairwise/metacatalog451/metacatalog451"
	"fmt"
)

func main() {
	s, _ := metacatalog451.New(metacatalog451.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog451.Batch{Ops: []metacatalog451.Op{{Kind: metacatalog451.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
