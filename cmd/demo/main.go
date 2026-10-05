package main

import (
	"example.com/pairwise/metacatalog351/metacatalog351"
	"fmt"
)

func main() {
	s, _ := metacatalog351.New(metacatalog351.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog351.Batch{Ops: []metacatalog351.Op{{Kind: metacatalog351.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
