package main

import (
	"example.com/pairwise/resourcecatalog131/resourcecatalog131"
	"fmt"
)

func main() {
	s, _ := resourcecatalog131.New(resourcecatalog131.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog131.Batch{Ops: []resourcecatalog131.Op{{Kind: resourcecatalog131.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
