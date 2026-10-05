package main

import (
	"example.com/pairwise/resourcecatalog151/resourcecatalog151"
	"fmt"
)

func main() {
	s, _ := resourcecatalog151.New(resourcecatalog151.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog151.Batch{Ops: []resourcecatalog151.Op{{Kind: resourcecatalog151.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
