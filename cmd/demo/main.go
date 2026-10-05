package main

import (
	"example.com/pairwise/modelcatalog/modelcatalog"
	"fmt"
)

func main() {
	s, _ := modelcatalog.New(modelcatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(modelcatalog.Batch{Ops: []modelcatalog.Op{{Kind: modelcatalog.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
