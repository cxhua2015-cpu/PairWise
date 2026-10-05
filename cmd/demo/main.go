package main

import (
	"example.com/pairwise/metacatalog311/metacatalog311"
	"fmt"
)

func main() {
	s, _ := metacatalog311.New(metacatalog311.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog311.Batch{Ops: []metacatalog311.Op{{Kind: metacatalog311.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
