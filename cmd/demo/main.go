package main

import (
	"example.com/pairwise/resourcecatalog146/resourcecatalog146"
	"fmt"
)

func main() {
	s, _ := resourcecatalog146.New(resourcecatalog146.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog146.Batch{Ops: []resourcecatalog146.Op{{Kind: resourcecatalog146.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
