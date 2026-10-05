package main

import (
	"example.com/pairwise/servicecatalog/servicecatalog"
	"fmt"
)

func main() {
	s, _ := servicecatalog.New(servicecatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(servicecatalog.Batch{Ops: []servicecatalog.Op{{Kind: servicecatalog.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
