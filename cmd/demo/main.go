package main

import (
	"example.com/pairwise/resourcecatalog121/resourcecatalog121"
	"fmt"
)

func main() {
	s, _ := resourcecatalog121.New(resourcecatalog121.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog121.Batch{Ops: []resourcecatalog121.Op{{Kind: resourcecatalog121.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
