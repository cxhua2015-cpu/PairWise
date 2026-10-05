package main

import (
	"example.com/pairwise/resourcecatalog106/resourcecatalog106"
	"fmt"
)

func main() {
	s, _ := resourcecatalog106.New(resourcecatalog106.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog106.Batch{Ops: []resourcecatalog106.Op{{Kind: resourcecatalog106.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
