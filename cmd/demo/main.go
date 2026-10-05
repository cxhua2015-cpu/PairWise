package main

import (
	"example.com/pairwise/metacatalog236/metacatalog236"
	"fmt"
)

func main() {
	s, _ := metacatalog236.New(metacatalog236.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog236.Batch{Ops: []metacatalog236.Op{{Kind: metacatalog236.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
