package main

import (
	"example.com/pairwise/metacatalog436/metacatalog436"
	"fmt"
)

func main() {
	s, _ := metacatalog436.New(metacatalog436.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(metacatalog436.Batch{Ops: []metacatalog436.Op{{Kind: metacatalog436.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
