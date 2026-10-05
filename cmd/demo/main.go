package main

import (
	"example.com/pairwise/metacatalog256/metacatalog256"
	"fmt"
)

func main() {
	s, _ := metacatalog256.New(metacatalog256.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog256.Batch{Ops: []metacatalog256.Op{{Kind: metacatalog256.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
