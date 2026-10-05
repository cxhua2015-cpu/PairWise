package main

import (
	"example.com/pairwise/metacatalog366/metacatalog366"
	"fmt"
)

func main() {
	s, _ := metacatalog366.New(metacatalog366.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog366.Batch{Ops: []metacatalog366.Op{{Kind: metacatalog366.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
