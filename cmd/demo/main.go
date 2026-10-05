package main

import (
	"example.com/pairwise/metacatalog281/metacatalog281"
	"fmt"
)

func main() {
	s, _ := metacatalog281.New(metacatalog281.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog281.Batch{Ops: []metacatalog281.Op{{Kind: metacatalog281.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
