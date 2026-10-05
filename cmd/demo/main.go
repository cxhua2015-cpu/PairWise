package main

import (
	"example.com/pairwise/resourcecatalog176/resourcecatalog176"
	"fmt"
)

func main() {
	s, _ := resourcecatalog176.New(resourcecatalog176.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog176.Batch{Ops: []resourcecatalog176.Op{{Kind: resourcecatalog176.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
