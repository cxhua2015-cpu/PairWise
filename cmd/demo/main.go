package main

import (
	"example.com/pairwise/metacatalog226/metacatalog226"
	"fmt"
)

func main() {
	s, _ := metacatalog226.New(metacatalog226.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog226.Batch{Ops: []metacatalog226.Op{{Kind: metacatalog226.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
