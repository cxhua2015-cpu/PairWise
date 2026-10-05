package main

import (
	"example.com/pairwise/resourcecatalog081/resourcecatalog081"
	"fmt"
)

func main() {
	s, _ := resourcecatalog081.New(resourcecatalog081.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog081.Batch{Ops: []resourcecatalog081.Op{{Kind: resourcecatalog081.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
