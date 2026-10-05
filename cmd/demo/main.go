package main

import (
	"example.com/pairwise/metacatalog216/metacatalog216"
	"fmt"
)

func main() {
	s, _ := metacatalog216.New(metacatalog216.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog216.Batch{Ops: []metacatalog216.Op{{Kind: metacatalog216.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
