package main

import (
	"example.com/pairwise/metacatalog356/metacatalog356"
	"fmt"
)

func main() {
	s, _ := metacatalog356.New(metacatalog356.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog356.Batch{Ops: []metacatalog356.Op{{Kind: metacatalog356.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
