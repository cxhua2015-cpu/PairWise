package main

import (
	"example.com/pairwise/metacatalog286/metacatalog286"
	"fmt"
)

func main() {
	s, _ := metacatalog286.New(metacatalog286.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog286.Batch{Ops: []metacatalog286.Op{{Kind: metacatalog286.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
