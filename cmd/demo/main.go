package main

import (
	"example.com/pairwise/resourcecatalog086/resourcecatalog086"
	"fmt"
)

func main() {
	s, _ := resourcecatalog086.New(resourcecatalog086.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog086.Batch{Ops: []resourcecatalog086.Op{{Kind: resourcecatalog086.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
