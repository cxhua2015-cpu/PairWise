package main

import (
	"example.com/pairwise/resourcecatalog111/resourcecatalog111"
	"fmt"
)

func main() {
	s, _ := resourcecatalog111.New(resourcecatalog111.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog111.Batch{Ops: []resourcecatalog111.Op{{Kind: resourcecatalog111.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
