package main

import (
	"example.com/pairwise/metacatalog271/metacatalog271"
	"fmt"
)

func main() {
	s, _ := metacatalog271.New(metacatalog271.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog271.Batch{Ops: []metacatalog271.Op{{Kind: metacatalog271.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
