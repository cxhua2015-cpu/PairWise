package main

import (
	"example.com/pairwise/metacatalog371/metacatalog371"
	"fmt"
)

func main() {
	s, _ := metacatalog371.New(metacatalog371.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog371.Batch{Ops: []metacatalog371.Op{{Kind: metacatalog371.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
