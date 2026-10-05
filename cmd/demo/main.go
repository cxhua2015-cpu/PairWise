package main

import (
	"example.com/pairwise/metacatalog441/metacatalog441"
	"fmt"
)

func main() {
	s, _ := metacatalog441.New(metacatalog441.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog441.Batch{Ops: []metacatalog441.Op{{Kind: metacatalog441.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
