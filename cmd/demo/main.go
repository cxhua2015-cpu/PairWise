package main

import (
	"example.com/pairwise/metacatalog331/metacatalog331"
	"fmt"
)

func main() {
	s, _ := metacatalog331.New(metacatalog331.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog331.Batch{Ops: []metacatalog331.Op{{Kind: metacatalog331.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
