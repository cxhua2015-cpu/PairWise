package main

import (
	"example.com/pairwise/metacatalog416/metacatalog416"
	"fmt"
)

func main() {
	s, _ := metacatalog416.New(metacatalog416.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog416.Batch{Ops: []metacatalog416.Op{{Kind: metacatalog416.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
