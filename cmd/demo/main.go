package main

import (
	"example.com/pairwise/resourcecatalog156/resourcecatalog156"
	"fmt"
)

func main() {
	s, _ := resourcecatalog156.New(resourcecatalog156.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog156.Batch{Ops: []resourcecatalog156.Op{{Kind: resourcecatalog156.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
