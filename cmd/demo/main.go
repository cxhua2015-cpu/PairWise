package main

import (
	"example.com/pairwise/metacatalog406/metacatalog406"
	"fmt"
)

func main() {
	s, _ := metacatalog406.New(metacatalog406.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog406.Batch{Ops: []metacatalog406.Op{{Kind: metacatalog406.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
