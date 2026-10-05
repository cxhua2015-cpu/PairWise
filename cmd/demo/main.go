package main

import (
	"example.com/pairwise/resourcecatalog191/resourcecatalog191"
	"fmt"
)

func main() {
	s, _ := resourcecatalog191.New(resourcecatalog191.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog191.Batch{Ops: []resourcecatalog191.Op{{Kind: resourcecatalog191.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
