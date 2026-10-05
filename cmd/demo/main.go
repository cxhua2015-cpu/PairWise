package main

import (
	"example.com/pairwise/metacatalog326/metacatalog326"
	"fmt"
)

func main() {
	s, _ := metacatalog326.New(metacatalog326.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog326.Batch{Ops: []metacatalog326.Op{{Kind: metacatalog326.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
