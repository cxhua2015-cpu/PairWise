package main

import (
	"example.com/pairwise/metacatalog266/metacatalog266"
	"fmt"
)

func main() {
	s, _ := metacatalog266.New(metacatalog266.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog266.Batch{Ops: []metacatalog266.Op{{Kind: metacatalog266.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
