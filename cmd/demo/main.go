package main

import (
	"example.com/pairwise/resourcecatalog096/resourcecatalog096"
	"fmt"
)

func main() {
	s, _ := resourcecatalog096.New(resourcecatalog096.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog096.Batch{Ops: []resourcecatalog096.Op{{Kind: resourcecatalog096.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
