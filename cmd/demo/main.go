package main

import (
	"example.com/pairwise/metacatalog421/metacatalog421"
	"fmt"
)

func main() {
	s, _ := metacatalog421.New(metacatalog421.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog421.Batch{Ops: []metacatalog421.Op{{Kind: metacatalog421.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
