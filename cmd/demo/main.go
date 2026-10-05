package main

import (
	"example.com/pairwise/metacatalog486/metacatalog486"
	"fmt"
)

func main() {
	s, _ := metacatalog486.New(metacatalog486.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog486.Batch{Ops: []metacatalog486.Op{{Kind: metacatalog486.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
