package main

import (
	"example.com/pairwise/resourcecatalog181/resourcecatalog181"
	"fmt"
)

func main() {
	s, _ := resourcecatalog181.New(resourcecatalog181.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog181.Batch{Ops: []resourcecatalog181.Op{{Kind: resourcecatalog181.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
