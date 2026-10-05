package main

import (
	"example.com/pairwise/metacatalog496/metacatalog496"
	"fmt"
)

func main() {
	s, _ := metacatalog496.New(metacatalog496.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog496.Batch{Ops: []metacatalog496.Op{{Kind: metacatalog496.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
