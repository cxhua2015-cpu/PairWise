package main

import (
	"example.com/pairwise/resourcecatalog186/resourcecatalog186"
	"fmt"
)

func main() {
	s, _ := resourcecatalog186.New(resourcecatalog186.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(resourcecatalog186.Batch{Ops: []resourcecatalog186.Op{{Kind: resourcecatalog186.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
