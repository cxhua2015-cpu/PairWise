package main

import (
	"example.com/pairwise/metacatalog361/metacatalog361"
	"fmt"
)

func main() {
	s, _ := metacatalog361.New(metacatalog361.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog361.Batch{Ops: []metacatalog361.Op{{Kind: metacatalog361.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
