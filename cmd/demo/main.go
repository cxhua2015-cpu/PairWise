package main

import (
	"example.com/pairwise/metacatalog476/metacatalog476"
	"fmt"
)

func main() {
	s, _ := metacatalog476.New(metacatalog476.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog476.Batch{Ops: []metacatalog476.Op{{Kind: metacatalog476.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
