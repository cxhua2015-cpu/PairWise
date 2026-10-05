package main

import (
	"example.com/pairwise/metacatalog316/metacatalog316"
	"fmt"
)

func main() {
	s, _ := metacatalog316.New(metacatalog316.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog316.Batch{Ops: []metacatalog316.Op{{Kind: metacatalog316.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
