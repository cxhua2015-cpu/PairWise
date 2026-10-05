package main

import (
	"example.com/pairwise/metacatalog481/metacatalog481"
	"fmt"
)

func main() {
	s, _ := metacatalog481.New(metacatalog481.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog481.Batch{Ops: []metacatalog481.Op{{Kind: metacatalog481.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
