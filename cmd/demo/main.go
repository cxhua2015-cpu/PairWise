package main

import (
	"example.com/pairwise/metacatalog396/metacatalog396"
	"fmt"
)

func main() {
	s, _ := metacatalog396.New(metacatalog396.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog396.Batch{Ops: []metacatalog396.Op{{Kind: metacatalog396.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
