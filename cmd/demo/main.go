package main

import (
	"example.com/pairwise/resourcecatalog101/resourcecatalog101"
	"fmt"
)

func main() {
	s, _ := resourcecatalog101.New(resourcecatalog101.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog101.Batch{Ops: []resourcecatalog101.Op{{Kind: resourcecatalog101.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
