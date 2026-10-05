package main

import (
	"example.com/pairwise/metacatalog211/metacatalog211"
	"fmt"
)

func main() {
	s, _ := metacatalog211.New(metacatalog211.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog211.Batch{Ops: []metacatalog211.Op{{Kind: metacatalog211.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
