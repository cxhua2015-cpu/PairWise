package main

import (
	"example.com/pairwise/metacatalog491/metacatalog491"
	"fmt"
)

func main() {
	s, _ := metacatalog491.New(metacatalog491.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog491.Batch{Ops: []metacatalog491.Op{{Kind: metacatalog491.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
