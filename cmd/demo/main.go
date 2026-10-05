package main

import (
	"example.com/pairwise/resourcecatalog161/resourcecatalog161"
	"fmt"
)

func main() {
	s, _ := resourcecatalog161.New(resourcecatalog161.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog161.Batch{Ops: []resourcecatalog161.Op{{Kind: resourcecatalog161.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
