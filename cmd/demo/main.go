package main

import (
	"example.com/pairwise/metacatalog386/metacatalog386"
	"fmt"
)

func main() {
	s, _ := metacatalog386.New(metacatalog386.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog386.Batch{Ops: []metacatalog386.Op{{Kind: metacatalog386.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
