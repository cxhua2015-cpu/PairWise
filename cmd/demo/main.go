package main

import (
	"example.com/pairwise/endpointcatalog/endpointcatalog"
	"fmt"
)

func main() {
	s, _ := endpointcatalog.New(endpointcatalog.Options{MaxRecords: 4, MaxNameBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	x, _ := s.Apply(endpointcatalog.Batch{Ops: []endpointcatalog.Op{{Kind: endpointcatalog.Put, Name: "alpha", Value: []byte("v")}}})
	fmt.Printf("generation=%d revision=%d records=%d\n", x.Generation, x.Revision, len(s.Snapshot().Records))
}
