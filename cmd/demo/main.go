package main

import (
	"example.com/pairwise/resourcecatalog136/resourcecatalog136"
	"fmt"
)

func main() {
	s, _ := resourcecatalog136.New(resourcecatalog136.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog136.Batch{Ops: []resourcecatalog136.Op{{Kind: resourcecatalog136.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
