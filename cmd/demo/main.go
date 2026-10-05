package main

import (
	"example.com/pairwise/metacatalog251/metacatalog251"
	"fmt"
)

func main() {
	s, _ := metacatalog251.New(metacatalog251.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog251.Batch{Ops: []metacatalog251.Op{{Kind: metacatalog251.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
