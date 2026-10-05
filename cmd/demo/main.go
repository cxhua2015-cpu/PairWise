package main

import (
	"example.com/pairwise/metacatalog306/metacatalog306"
	"fmt"
)

func main() {
	s, _ := metacatalog306.New(metacatalog306.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog306.Batch{Ops: []metacatalog306.Op{{Kind: metacatalog306.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
