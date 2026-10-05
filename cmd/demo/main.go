package main

import (
	"example.com/pairwise/resourcecatalog116/resourcecatalog116"
	"fmt"
)

func main() {
	s, _ := resourcecatalog116.New(resourcecatalog116.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog116.Batch{Ops: []resourcecatalog116.Op{{Kind: resourcecatalog116.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
