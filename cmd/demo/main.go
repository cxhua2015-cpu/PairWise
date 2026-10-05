package main

import (
	"example.com/pairwise/metacatalog456/metacatalog456"
	"fmt"
)

func main() {
	s, _ := metacatalog456.New(metacatalog456.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog456.Batch{Ops: []metacatalog456.Op{{Kind: metacatalog456.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
