package main

import (
	"example.com/pairwise/resourcecatalog091/resourcecatalog091"
	"fmt"
)

func main() {
	s, _ := resourcecatalog091.New(resourcecatalog091.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog091.Batch{Ops: []resourcecatalog091.Op{{Kind: resourcecatalog091.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
