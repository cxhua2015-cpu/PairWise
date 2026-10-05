package main

import (
	"example.com/pairwise/metacatalog276/metacatalog276"
	"fmt"
)

func main() {
	s, _ := metacatalog276.New(metacatalog276.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog276.Batch{Ops: []metacatalog276.Op{{Kind: metacatalog276.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
