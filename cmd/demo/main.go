package main

import (
	"example.com/pairwise/metacatalog201/metacatalog201"
	"fmt"
)

func main() {
	s, _ := metacatalog201.New(metacatalog201.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog201.Batch{Ops: []metacatalog201.Op{{Kind: metacatalog201.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
