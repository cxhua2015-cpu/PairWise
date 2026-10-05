package main

import (
	"example.com/pairwise/metacatalog301/metacatalog301"
	"fmt"
)

func main() {
	s, _ := metacatalog301.New(metacatalog301.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog301.Batch{Ops: []metacatalog301.Op{{Kind: metacatalog301.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
