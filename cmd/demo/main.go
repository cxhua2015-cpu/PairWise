package main

import (
	"example.com/pairwise/metacatalog296/metacatalog296"
	"fmt"
)

func main() {
	s, _ := metacatalog296.New(metacatalog296.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog296.Batch{Ops: []metacatalog296.Op{{Kind: metacatalog296.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
