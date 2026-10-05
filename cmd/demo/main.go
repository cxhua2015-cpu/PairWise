package main

import (
	"example.com/pairwise/metacatalog261/metacatalog261"
	"fmt"
)

func main() {
	s, _ := metacatalog261.New(metacatalog261.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog261.Batch{Ops: []metacatalog261.Op{{Kind: metacatalog261.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
