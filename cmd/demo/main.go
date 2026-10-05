package main

import (
	"example.com/pairwise/metacatalog231/metacatalog231"
	"fmt"
)

func main() {
	s, _ := metacatalog231.New(metacatalog231.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog231.Batch{Ops: []metacatalog231.Op{{Kind: metacatalog231.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
