package main

import (
	"example.com/pairwise/metacatalog391/metacatalog391"
	"fmt"
)

func main() {
	s, _ := metacatalog391.New(metacatalog391.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog391.Batch{Ops: []metacatalog391.Op{{Kind: metacatalog391.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
