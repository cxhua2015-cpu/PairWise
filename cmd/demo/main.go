package main

import (
	"example.com/pairwise/metacatalog246/metacatalog246"
	"fmt"
)

func main() {
	s, _ := metacatalog246.New(metacatalog246.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog246.Batch{Ops: []metacatalog246.Op{{Kind: metacatalog246.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
