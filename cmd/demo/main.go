package main

import (
	"example.com/pairwise/metacatalog426/metacatalog426"
	"fmt"
)

func main() {
	s, _ := metacatalog426.New(metacatalog426.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog426.Batch{Ops: []metacatalog426.Op{{Kind: metacatalog426.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
