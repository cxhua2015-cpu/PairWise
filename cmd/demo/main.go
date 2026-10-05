package main

import (
	"example.com/pairwise/resourcecatalog141/resourcecatalog141"
	"fmt"
)

func main() {
	s, _ := resourcecatalog141.New(resourcecatalog141.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog141.Batch{Ops: []resourcecatalog141.Op{{Kind: resourcecatalog141.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
