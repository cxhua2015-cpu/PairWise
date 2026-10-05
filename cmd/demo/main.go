package main

import (
	"example.com/pairwise/metacatalog241/metacatalog241"
	"fmt"
)

func main() {
	s, _ := metacatalog241.New(metacatalog241.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog241.Batch{Ops: []metacatalog241.Op{{Kind: metacatalog241.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
