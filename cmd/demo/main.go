package main

import (
	"example.com/pairwise/metacatalog341/metacatalog341"
	"fmt"
)

func main() {
	s, _ := metacatalog341.New(metacatalog341.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog341.Batch{Ops: []metacatalog341.Op{{Kind: metacatalog341.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
