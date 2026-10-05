package main

import (
	"example.com/pairwise/metacatalog376/metacatalog376"
	"fmt"
)

func main() {
	s, _ := metacatalog376.New(metacatalog376.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog376.Batch{Ops: []metacatalog376.Op{{Kind: metacatalog376.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
