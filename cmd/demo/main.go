package main

import (
	"example.com/pairwise/metacatalog431/metacatalog431"
	"fmt"
)

func main() {
	s, _ := metacatalog431.New(metacatalog431.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog431.Batch{Ops: []metacatalog431.Op{{Kind: metacatalog431.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
