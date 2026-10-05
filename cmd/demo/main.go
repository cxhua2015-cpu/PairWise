package main

import (
	"example.com/pairwise/policycatalog/policycatalog"
	"fmt"
)

func main() {
	s, _ := policycatalog.New(policycatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(policycatalog.Batch{Ops: []policycatalog.Op{{Kind: policycatalog.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
