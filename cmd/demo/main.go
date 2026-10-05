package main

import (
	"example.com/pairwise/metacatalog466/metacatalog466"
	"fmt"
)

func main() {
	s, _ := metacatalog466.New(metacatalog466.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog466.Batch{Ops: []metacatalog466.Op{{Kind: metacatalog466.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
