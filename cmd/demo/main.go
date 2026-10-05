package main

import (
	"example.com/pairwise/metacatalog346/metacatalog346"
	"fmt"
)

func main() {
	s, _ := metacatalog346.New(metacatalog346.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog346.Batch{Ops: []metacatalog346.Op{{Kind: metacatalog346.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
