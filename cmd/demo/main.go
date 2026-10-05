package main

import (
	"example.com/pairwise/resourcecatalog196/resourcecatalog196"
	"fmt"
)

func main() {
	s, _ := resourcecatalog196.New(resourcecatalog196.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog196.Batch{Ops: []resourcecatalog196.Op{{Kind: resourcecatalog196.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
