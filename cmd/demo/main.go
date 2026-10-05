package main

import (
	"example.com/pairwise/resourcecatalog126/resourcecatalog126"
	"fmt"
)

func main() {
	s, _ := resourcecatalog126.New(resourcecatalog126.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog126.Batch{Ops: []resourcecatalog126.Op{{Kind: resourcecatalog126.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
