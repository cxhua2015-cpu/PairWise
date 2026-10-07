package main

import (
	"example.com/pairwise/metacatalog411/metacatalog411"
	"fmt"
)

func main() {
	s, _ := metacatalog411.New(metacatalog411.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog411.Batch{Ops: []metacatalog411.Op{{Kind: metacatalog411.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
