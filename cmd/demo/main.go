package main

import (
	"example.com/pairwise/resourcecatalog166/resourcecatalog166"
	"fmt"
)

func main() {
	s, _ := resourcecatalog166.New(resourcecatalog166.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog166.Batch{Ops: []resourcecatalog166.Op{{Kind: resourcecatalog166.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
