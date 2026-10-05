package main

import (
	"example.com/pairwise/metacatalog401/metacatalog401"
	"fmt"
)

func main() {
	s, _ := metacatalog401.New(metacatalog401.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog401.Batch{Ops: []metacatalog401.Op{{Kind: metacatalog401.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
