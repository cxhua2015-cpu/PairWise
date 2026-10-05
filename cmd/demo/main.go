package main

import (
	"example.com/pairwise/metacatalog221/metacatalog221"
	"fmt"
)

func main() {
	s, _ := metacatalog221.New(metacatalog221.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog221.Batch{Ops: []metacatalog221.Op{{Kind: metacatalog221.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
