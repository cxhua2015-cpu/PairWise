package main

import (
	"example.com/pairwise/resourcecatalog171/resourcecatalog171"
	"fmt"
)

func main() {
	s, _ := resourcecatalog171.New(resourcecatalog171.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog171.Batch{Ops: []resourcecatalog171.Op{{Kind: resourcecatalog171.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
